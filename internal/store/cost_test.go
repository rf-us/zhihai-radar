package store

import (
	"context"
	"math"
	"testing"

	"github.com/rf-us/zhihai-radar/internal/domain"
)

func TestSaveCostRecomputesAndCanBeRemoved(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	o := &Opportunity{Keyword: "咖啡机", Name: "咖啡机", Scores: domain.Scores{
		Demand: 90, Pain: 80, Trend: 70, Competition: 20, Confidence: 0.8,
	}, Verdict: "需进一步验证"}
	if err := st.CreateOpportunity(ctx, o); err != nil {
		t.Fatal(err)
	}

	// 保存成本：confidence 应到 1.0。
	bom := []domain.CostLine{{PartName: "外壳", Qty: 2, UnitPrice: 40, LossRate: 5}}
	target := 200.0
	cb, ok := domain.EstimateCost(bom, &target, nil)
	if !ok {
		t.Fatal("样例成本应可算")
	}
	if err := st.SaveCost(ctx, o.ID, bom, cb); err != nil {
		t.Fatal(err)
	}
	got, err := st.GetOpportunity(ctx, o.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Scores.Cost == nil || math.Abs(got.Scores.Confidence-1.0) > 1e-9 {
		t.Fatalf("填成本后应 confidence=1.0：%+v", got.Scores)
	}
	if got.Verdict != "值得验证" {
		t.Fatalf("填成本后应判值得验证：%s", got.Verdict)
	}

	rec, err := st.LatestCost(ctx, o.ID)
	if err != nil || rec == nil || len(rec.BOM) != 1 {
		t.Fatalf("LatestCost: %+v %v", rec, err)
	}

	// 删除成本：confidence 回 0.8，结论回落。
	if err := st.DeleteCost(ctx, o.ID); err != nil {
		t.Fatal(err)
	}
	got, _ = st.GetOpportunity(ctx, o.ID)
	if math.Abs(got.Scores.Confidence-0.8) > 1e-9 || got.Scores.Cost != nil {
		t.Fatalf("删成本后应回 0.8 且 D5 为空：%+v", got.Scores)
	}
	if got.Verdict != "需进一步验证" {
		t.Fatalf("删成本后不应判值得验证：%s", got.Verdict)
	}
}

func TestUseModeDivideByThousand(t *testing.T) {
	// use_mode=1 且 Qty=1000：每 1000 台用 1 个，单价 300 → 单台 0.3。
	cb, ok := domain.EstimateCost([]domain.CostLine{{Qty: 1000, UnitPrice: 300, UseMode: 1}}, nil, nil)
	if !ok || math.Abs(cb.TotalCost-0.3) > 1e-9 {
		t.Fatalf("use_mode 除法错误：%+v ok=%v", cb, ok)
	}
}

func TestSaveCostUnknownOpportunity(t *testing.T) {
	st := newTestStore(t)
	if err := st.SaveCost(context.Background(), 99999, nil, domain.CostBreakdown{Score: 50}); err != ErrNotFound {
		t.Fatalf("未知机会应报 ErrNotFound，got %v", err)
	}
}
