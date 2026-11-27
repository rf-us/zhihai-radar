package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/rf-us/zhihai-radar/internal/dialogue"
	"github.com/rf-us/zhihai-radar/internal/domain"
)

var ErrNotFound = errors.New("store: not found")

type Opportunity struct {
	ID        int64            `json:"id"`
	Keyword   string           `json:"keyword"`
	Name      string           `json:"name"`
	OneLine   string           `json:"one_line"`
	Category  string           `json:"category"`
	Scores    domain.Scores    `json:"scores"`
	Verdict   string           `json:"verdict"`
	Reasons   []string         `json:"reasons"`
	PainJSON  string           `json:"pain_points_json"`
	Status    string           `json:"status"`
	CreatedAt time.Time        `json:"created_at"`
	UpdatedAt time.Time        `json:"updated_at"`
	Analysis  *AnalysisContext `json:"analysis,omitempty"`
}

func (o Opportunity) ReasonsWithCost(record *CostRecord) []string {
	if record == nil || o.Scores.Cost == nil {
		return o.Reasons
	}
	reasons := make([]string, 0, len(o.Reasons)+1)
	for _, reason := range o.Reasons {
		if !strings.HasPrefix(reason, "D5 ") && !strings.HasPrefix(reason, "D5=") {
			reasons = append(reasons, reason)
		}
	}
	cost := record.Breakdown
	return append(reasons, fmt.Sprintf("D5=%.2f：单台成本 ¥%.2f，建议售价 ¥%.2f（%s），毛利 %.2f%%",
		*o.Scores.Cost, cost.TotalCost, cost.Suggested, cost.PriceSource, cost.Margin*100))
}

type AnalysisContext struct {
	Plan    *dialogue.Plan            `json:"plan,omitempty"`
	Mode    string                    `json:"mode"`
	Sources map[string]SourceProgress `json:"sources"`
}

type SourceProgress struct {
	Status string `json:"status"`
	Reason string `json:"reason,omitempty"`
}

type Adoption struct {
	ID             int64
	OpportunityID  int64
	ReefBase       string
	ReefDatabaseID string
	ProductID      int64
	BOMCreated     int
	BOMSkipped     int
	Status         string
	Message        string
	Operator       string
	CreatedAt      time.Time
}

func (s *Store) CreateOpportunity(ctx context.Context, o *Opportunity) error {
	return createOpportunity(ctx, s.db, o)
}

func (s *Store) SaveAnalysis(ctx context.Context, o *Opportunity, evidence []Evidence) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	saved := *o
	if err := createOpportunity(ctx, tx, &saved); err != nil {
		return err
	}
	if err := saveEvidence(ctx, tx, saved.ID, evidence); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	*o = saved
	return nil
}

type opportunityExecer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func createOpportunity(ctx context.Context, exec opportunityExecer, o *Opportunity) error {
	now := time.Now().UTC()
	if o.CreatedAt.IsZero() {
		o.CreatedAt = now
	}
	o.UpdatedAt = now
	if o.Status == "" {
		o.Status = "analyzed"
	}
	reasons, err := json.Marshal(o.Reasons)
	if err != nil {
		return err
	}
	analysis, err := json.Marshal(o.Analysis)
	if err != nil {
		return err
	}
	if o.PainJSON == "" {
		o.PainJSON = "[]"
	}

	var cost any
	if o.Scores.Cost != nil {
		cost = *o.Scores.Cost
	}

	res, err := exec.ExecContext(ctx, `INSERT INTO opportunity
		(keyword,name,one_line,category,score_demand,score_pain,score_trend,score_competition,
		 score_cost,score_overall,confidence,verdict,reasons_json,pain_points_json,status,created_at,updated_at,analysis_context_json)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		o.Keyword, o.Name, o.OneLine, o.Category,
		o.Scores.Demand, o.Scores.Pain, o.Scores.Trend, o.Scores.Competition,
		cost, o.Scores.Overall, o.Scores.Confidence, o.Verdict,
		string(reasons), o.PainJSON, o.Status,
		o.CreatedAt.Format(time.RFC3339), o.UpdatedAt.Format(time.RFC3339), string(analysis))
	if err != nil {
		return fmt.Errorf("store: create opportunity: %w", err)
	}
	o.ID, err = res.LastInsertId()
	return err
}

func (s *Store) GetOpportunity(ctx context.Context, id int64) (*Opportunity, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id,keyword,name,one_line,category,
		score_demand,score_pain,score_trend,score_competition,score_cost,score_overall,confidence,
		verdict,reasons_json,pain_points_json,status,created_at,updated_at,analysis_context_json
		FROM opportunity WHERE id=?`, id)
	return scanOpportunity(row)
}

func (s *Store) ListOpportunities(ctx context.Context, limit int) ([]Opportunity, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,keyword,name,one_line,category,
		score_demand,score_pain,score_trend,score_competition,score_cost,score_overall,confidence,
		verdict,reasons_json,pain_points_json,status,created_at,updated_at,analysis_context_json
		FROM opportunity ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]Opportunity, 0, limit)
	for rows.Next() {
		o, err := scanOpportunity(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *o)
	}
	return out, rows.Err()
}

type scanner interface {
	Scan(dest ...any) error
}

func scanOpportunity(sc scanner) (*Opportunity, error) {
	var o Opportunity
	var cost sql.NullFloat64
	var reasons, analysis string
	var created, updated string

	err := sc.Scan(&o.ID, &o.Keyword, &o.Name, &o.OneLine, &o.Category,
		&o.Scores.Demand, &o.Scores.Pain, &o.Scores.Trend, &o.Scores.Competition,
		&cost, &o.Scores.Overall, &o.Scores.Confidence,
		&o.Verdict, &reasons, &o.PainJSON, &o.Status, &created, &updated, &analysis)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if cost.Valid {
		v := cost.Float64
		o.Scores.Cost = &v
	}
	_ = json.Unmarshal([]byte(reasons), &o.Reasons)
	if err := json.Unmarshal([]byte(analysis), &o.Analysis); err != nil {
		return nil, fmt.Errorf("store: read analysis context: %w", err)
	}
	o.CreatedAt, _ = time.Parse(time.RFC3339, created)
	o.UpdatedAt, _ = time.Parse(time.RFC3339, updated)
	return &o, nil
}

func (s *Store) SaveAdoption(ctx context.Context, a *Adoption) error {
	if a.CreatedAt.IsZero() {
		a.CreatedAt = time.Now().UTC()
	}
	res, err := s.db.ExecContext(ctx, `INSERT INTO adoption
		(opportunity_id,reef_base,reef_database_id,product_id,bom_created,bom_skipped,status,message,operator,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?)`,
		a.OpportunityID, a.ReefBase, a.ReefDatabaseID, a.ProductID,
		a.BOMCreated, a.BOMSkipped, a.Status, a.Message, a.Operator,
		a.CreatedAt.Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("store: save adoption: %w", err)
	}
	a.ID, _ = res.LastInsertId()
	return nil
}

// SuccessfulAdoption 返回某候选的成功采纳记录；没有则返回 (nil, nil)。
func (s *Store) SuccessfulAdoption(ctx context.Context, opportunityID int64) (*Adoption, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id,opportunity_id,reef_base,reef_database_id,product_id,
		bom_created,bom_skipped,status,message,operator,created_at
		FROM adoption WHERE opportunity_id=? AND status='ok' LIMIT 1`, opportunityID)
	var a Adoption
	var created string
	err := row.Scan(&a.ID, &a.OpportunityID, &a.ReefBase, &a.ReefDatabaseID, &a.ProductID,
		&a.BOMCreated, &a.BOMSkipped, &a.Status, &a.Message, &a.Operator, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	a.CreatedAt, _ = time.Parse(time.RFC3339, created)
	return &a, nil
}
