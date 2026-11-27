package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/rf-us/zhihai-radar/internal/domain"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "radar.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() {
		_ = st.Close()
		// Windows 上 SQLite 关闭后 WAL/SHM 落盘有极短延迟；t.TempDir 的清理
		// 会立刻删目录，偶发 "directory is not empty"。等一拍再让 TempDir 删。
		time.Sleep(30 * time.Millisecond)
	})
	if err := st.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return st
}

func TestMigrateIsIdempotent(t *testing.T) {
	st := newTestStore(t)
	if err := st.Migrate(context.Background()); err != nil {
		t.Fatalf("第二次迁移不应失败: %v", err)
	}
}

func TestOpportunityRoundTrip(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	cost := 88.0
	o := &Opportunity{
		Keyword: "便携咖啡机", Name: "便携咖啡机", Category: "家居小家电",
		Scores:  domain.Scores{Demand: 85, Pain: 72, Trend: 60, Competition: 30, Cost: &cost, Overall: 78, Confidence: 1},
		Verdict: "值得验证",
		Reasons: []string{"D1=85：命中 10 条"},
	}
	if err := st.CreateOpportunity(ctx, o); err != nil {
		t.Fatalf("create: %v", err)
	}
	if o.ID == 0 {
		t.Fatal("应回填 ID")
	}

	got, err := st.GetOpportunity(ctx, o.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Keyword != o.Keyword || got.Verdict != o.Verdict || len(got.Reasons) != 1 {
		t.Fatalf("读回不一致: %+v", got)
	}
	if got.Scores.Cost == nil || *got.Scores.Cost != 88 {
		t.Fatalf("成本应保留: %+v", got.Scores)
	}

	list, err := st.ListOpportunities(ctx, 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("期望 1 条，得到 %d", len(list))
	}
	if list == nil {
		t.Fatal("列表必须是空切片而不是 nil")
	}
}

func TestNullCostStaysNull(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	o := &Opportunity{Keyword: "k", Name: "n"}
	if err := st.CreateOpportunity(ctx, o); err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := st.GetOpportunity(ctx, o.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Scores.Cost != nil {
		t.Fatalf("未测算时应为 nil，得到 %v", *got.Scores.Cost)
	}
}

func TestOnlyOneSuccessfulAdoption(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	o := &Opportunity{Keyword: "k", Name: "n"}
	if err := st.CreateOpportunity(ctx, o); err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := st.SaveAdoption(ctx, &Adoption{OpportunityID: o.ID, Status: "failed"}); err != nil {
		t.Fatalf("失败记录应可写入: %v", err)
	}
	if err := st.SaveAdoption(ctx, &Adoption{OpportunityID: o.ID, Status: "ok", ProductID: 1}); err != nil {
		t.Fatalf("首次成功应可写入: %v", err)
	}
	if err := st.SaveAdoption(ctx, &Adoption{OpportunityID: o.ID, Status: "ok", ProductID: 2}); err == nil {
		t.Fatal("第二条成功记录必须被部分唯一索引挡住")
	}

	ok, err := st.SuccessfulAdoption(ctx, o.ID)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if ok == nil || ok.ProductID != 1 {
		t.Fatalf("应查到第一次成功的记录: %+v", ok)
	}
}

func TestNotFound(t *testing.T) {
	st := newTestStore(t)
	if _, err := st.GetOpportunity(context.Background(), 999); err != ErrNotFound {
		t.Fatalf("期望 ErrNotFound，得到 %v", err)
	}
}
