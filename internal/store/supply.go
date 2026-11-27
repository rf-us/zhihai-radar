package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/rf-us/zhihai-radar/internal/supply"
)

// ErrQuoted 是删除供应商时仍有报价引用的冲突（HTTP 层映射成 409）。
// 携带引用它的机会 ID，供界面说明"哪些机会还在用这家供应商"。
type ErrQuoted struct {
	OpportunityIDs []int64
}

func (e *ErrQuoted) Error() string {
	return fmt.Sprintf("store: supplier is quoted by opportunities %v", e.OpportunityIDs)
}

// CreateSupplier 新增供应商。
func (s *Store) CreateSupplier(ctx context.Context, in supply.Supplier) (supply.Supplier, error) {
	if strings.TrimSpace(in.Name) == "" {
		return in, errors.New("供应商名称不能为空")
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO supplier(name,region,contact,payment_terms,note,created_at) VALUES(?,?,?,?,?,?)`,
		in.Name, in.Region, in.Contact, in.PaymentTerms, in.Note, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return in, fmt.Errorf("store: create supplier: %w", err)
	}
	id, _ := res.LastInsertId()
	in.ID = id
	return in, nil
}

// ListSuppliers 返回全部供应商（按名称稳定排序）。
func (s *Store) ListSuppliers(ctx context.Context) ([]supply.Supplier, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id,name,region,contact,payment_terms,note FROM supplier ORDER BY name,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]supply.Supplier, 0)
	for rows.Next() {
		var v supply.Supplier
		if err := rows.Scan(&v.ID, &v.Name, &v.Region, &v.Contact, &v.PaymentTerms, &v.Note); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// DeleteSupplier 删除未被报价引用的供应商；有引用时返回 ErrQuoted。
func (s *Store) DeleteSupplier(ctx context.Context, id int64) error {
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT opportunity_id FROM supplier_quote WHERE supplier_id=?`, id)
	if err != nil {
		return err
	}
	var ids []int64
	for rows.Next() {
		var oid int64
		if err := rows.Scan(&oid); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, oid)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if len(ids) > 0 {
		return &ErrQuoted{OpportunityIDs: ids}
	}
	_, err = s.db.ExecContext(ctx, `DELETE FROM supplier WHERE id=?`, id)
	return err
}

// CreateQuote 新增报价。tier_json 存原始 JSON 数组。
func (s *Store) CreateQuote(ctx context.Context, q supply.Quote) (supply.Quote, error) {
	if err := q.Validate(); err != nil {
		return q, err
	}
	// 确认机会存在，避免外键报错不友好。
	var exists int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM opportunity WHERE id=?`, q.OpportunityID).Scan(&exists); err != nil {
		return q, err
	}
	if exists == 0 {
		return q, ErrNotFound
	}
	tiers, err := json.Marshal(q.Tiers)
	if err != nil {
		return q, err
	}
	if string(tiers) == "null" {
		tiers = []byte("[]")
	}
	res, err := s.db.ExecContext(ctx, `INSERT INTO supplier_quote
		(opportunity_id,supplier_id,part_name,unit_price,currency,moq,lead_time_days,tier_json,selected,created_at)
		VALUES(?,?,?,?,?,?,?,?,0,?)`,
		q.OpportunityID, q.SupplierID, q.PartName, q.UnitPrice, q.Currency, q.MOQ, q.LeadTimeDays,
		string(tiers), time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return q, fmt.Errorf("store: create quote: %w", err)
	}
	q.ID, _ = res.LastInsertId()
	return q, nil
}

// ListQuotes 按机会列出报价，join 出供应商名。
func (s *Store) ListQuotes(ctx context.Context, opportunityID int64) ([]supply.Quote, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT q.id,q.opportunity_id,q.supplier_id,s.name,
		q.part_name,q.unit_price,q.currency,q.moq,q.lead_time_days,q.tier_json,q.selected
		FROM supplier_quote q JOIN supplier s ON s.id=q.supplier_id
		WHERE q.opportunity_id=? ORDER BY q.id`, opportunityID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]supply.Quote, 0)
	for rows.Next() {
		q, err := scanQuote(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, q)
	}
	return out, rows.Err()
}

// SelectQuote 在事务内先取消其它 selected，再选中指定报价。
//
// 部分唯一索引 uk_quote_selected 保证并发下不会出现两条 selected=1；
// 这里用事务把"取消旧的 + 选中新的"合成一个原子操作。
func (s *Store) SelectQuote(ctx context.Context, opportunityID, quoteID int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var qOpportunity int64
	if err := tx.QueryRowContext(ctx, `SELECT opportunity_id FROM supplier_quote WHERE id=?`, quoteID).Scan(&qOpportunity); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if qOpportunity != opportunityID {
		return errors.New("报价不属于该机会")
	}
	if _, err := tx.ExecContext(ctx, `UPDATE supplier_quote SET selected=0 WHERE opportunity_id=? AND selected=1`, opportunityID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE supplier_quote SET selected=1 WHERE id=?`, quoteID); err != nil {
		return err
	}
	return tx.Commit()
}

type quoteScanner interface{ Scan(dest ...any) error }

func scanQuote(row quoteScanner) (supply.Quote, error) {
	var q supply.Quote
	var tierJSON string
	var selected int
	if err := row.Scan(&q.ID, &q.OpportunityID, &q.SupplierID, &q.SupplierName,
		&q.PartName, &q.UnitPrice, &q.Currency, &q.MOQ, &q.LeadTimeDays, &tierJSON, &selected); err != nil {
		return q, err
	}
	q.Selected = selected == 1
	if tierJSON != "" && tierJSON != "null" {
		if err := json.Unmarshal([]byte(tierJSON), &q.Tiers); err != nil {
			return q, fmt.Errorf("store: decode tier_json: %w", err)
		}
	}
	return q, nil
}
