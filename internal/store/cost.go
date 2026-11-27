package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/rf-us/zhihai-radar/internal/domain"
)

// CostRecord 是一份已落库的成本测算。
type CostRecord struct {
	ID            int64                `json:"id"`
	OpportunityID int64                `json:"opportunity_id"`
	BOM           []domain.CostLine    `json:"bom"`
	Breakdown     domain.CostBreakdown `json:"breakdown"`
	Extras        domain.CostExtras    `json:"extras"`
	CreatedAt     time.Time            `json:"created_at"`
}

// SaveCost 保存成本测算并在同一事务内回写机会的 D5、综合分、置信度与结论。
//
// calc_version 通过 price_source 之外的独立字段区分算法版本时再用；
// 当前算法只有一个版本（domain.EstimateCost），先用 bom_json + 时间可追溯。
func (s *Store) SaveCost(ctx context.Context, opportunityID int64, bom []domain.CostLine, cb domain.CostBreakdown) error {
	return s.SaveCostWithExtras(ctx, opportunityID, bom, cb, domain.CostExtras{})
}

// SaveCostWithExtras 与 SaveCost 相同，但额外落库包装/运费/佣金率。
func (s *Store) SaveCostWithExtras(ctx context.Context, opportunityID int64, bom []domain.CostLine, cb domain.CostBreakdown, extras domain.CostExtras) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM opportunity WHERE id=?`, opportunityID).Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		return ErrNotFound
	}
	bomJSON, err := json.Marshal(bom)
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := tx.ExecContext(ctx, `INSERT INTO cost_model
		(opportunity_id,bom_json,total_cost,suggested_price,margin,price_source,packaging,freight,commission_rate,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?)`,
		opportunityID, string(bomJSON), cb.TotalCost, cb.Suggested, cb.Margin, cb.PriceSource,
		extras.Packaging, extras.Freight, extras.CommissionRate, now); err != nil {
		return fmt.Errorf("store: save cost: %w", err)
	}

	// 读取当前四维分，用 RecomputeWithCost 重算。
	var d, p, t, comp float64
	if err := tx.QueryRowContext(ctx,
		`SELECT score_demand,score_pain,score_trend,score_competition FROM opportunity WHERE id=?`, opportunityID,
	).Scan(&d, &p, &t, &comp); err != nil {
		return err
	}
	base := domain.Scores{Demand: d, Pain: p, Trend: t, Competition: comp}
	cost := cb.Score
	updated, verdict := domain.RecomputeWithCost(base, &cost)
	if _, err := tx.ExecContext(ctx,
		`UPDATE opportunity SET score_cost=?,score_overall=?,confidence=?,verdict=?,updated_at=? WHERE id=?`,
		cost, updated.Overall, updated.Confidence, verdict, now, opportunityID); err != nil {
		return err
	}
	return tx.Commit()
}

// DeleteCost 删除某机会的全部成本记录并重算分数（D5 置空、confidence 回 0.8）。
func (s *Store) DeleteCost(ctx context.Context, opportunityID int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var d, p, t, comp float64
	if err := tx.QueryRowContext(ctx,
		`SELECT score_demand,score_pain,score_trend,score_competition FROM opportunity WHERE id=?`, opportunityID,
	).Scan(&d, &p, &t, &comp); err != nil {
		if err == sql.ErrNoRows {
			return ErrNotFound
		}
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM cost_model WHERE opportunity_id=?`, opportunityID); err != nil {
		return err
	}
	base := domain.Scores{Demand: d, Pain: p, Trend: t, Competition: comp}
	updated, verdict := domain.RecomputeWithCost(base, nil)
	if _, err := tx.ExecContext(ctx,
		`UPDATE opportunity SET score_cost=NULL,score_overall=?,confidence=?,verdict=?,updated_at=? WHERE id=?`,
		updated.Overall, updated.Confidence, verdict, time.Now().UTC().Format(time.RFC3339), opportunityID); err != nil {
		return err
	}
	return tx.Commit()
}

// LatestCost 返回某机会最新一份成本记录；没有则返回 (nil, nil)。
func (s *Store) LatestCost(ctx context.Context, opportunityID int64) (*CostRecord, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id,opportunity_id,bom_json,total_cost,suggested_price,margin,price_source,packaging,freight,commission_rate,created_at
		FROM cost_model WHERE opportunity_id=? ORDER BY created_at DESC,id DESC LIMIT 1`, opportunityID)
	var r CostRecord
	var bomJSON, created string
	var cb domain.CostBreakdown
	err := row.Scan(&r.ID, &r.OpportunityID, &bomJSON, &cb.TotalCost, &cb.Suggested, &cb.Margin, &cb.PriceSource,
		&r.Extras.Packaging, &r.Extras.Freight, &r.Extras.CommissionRate, &created)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(bomJSON), &r.BOM); err != nil {
		return nil, fmt.Errorf("store: decode bom_json: %w", err)
	}
	r.CreatedAt, _ = time.Parse(time.RFC3339, created)
	r.Breakdown = cb
	return &r, nil
}
