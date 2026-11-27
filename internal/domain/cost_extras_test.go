package domain

import (
	"math"
	"testing"
)

func TestEstimateCostWithExtras(t *testing.T) {
	lines := []CostLine{
		{PartName: "外壳", Qty: 2, UnitPrice: 40, LossRate: 5},
		{PartName: "滤网", Qty: 1, UnitPrice: 12, LossRate: 0},
		{PartName: "加热模块", Qty: 1, UnitPrice: 36, LossRate: 2},
	}
	extras := CostExtras{Packaging: 8, Freight: 12, CommissionRate: 15}
	target := 200.0
	cb, ok := EstimateCostWithExtras(lines, &target, nil, extras)
	if !ok {
		t.Fatal("应可计算")
	}
	// BOM: 84+12+36.72=132.72；+包装8+运费12=152.72
	if math.Abs(cb.TotalCost-152.72) > 0.01 {
		t.Fatalf("总成本错误：%.2f", cb.TotalCost)
	}
	// 售价 200，佣金 15% → 净收入 170；毛利 (170-152.72)/200 = 8.64%
	want := (170 - 152.72) / 200
	if math.Abs(cb.Margin-want) > 0.001 {
		t.Fatalf("佣金后毛利错误：%.4f want %.4f", cb.Margin, want)
	}
}

func TestEstimateCostWithExtrasRejectsCommissionOver100(t *testing.T) {
	lines := []CostLine{{Qty: 1, UnitPrice: 10}}
	if _, ok := EstimateCostWithExtras(lines, nil, nil, CostExtras{CommissionRate: 101}); ok {
		t.Fatal("佣金 >100% 应拒绝")
	}
	if err := (CostExtras{Freight: -1}).Validate(); err == nil {
		t.Fatal("负运费应拒绝")
	}
}

func TestNoExtrasMatchesLegacy(t *testing.T) {
	lines := []CostLine{{Qty: 2, UnitPrice: 40, LossRate: 5}}
	target := 200.0
	legacy, _ := EstimateCost(lines, &target, nil)
	with, _ := EstimateCostWithExtras(lines, &target, nil, CostExtras{})
	if legacy.TotalCost != with.TotalCost || legacy.Score != with.Score {
		t.Fatalf("无附加项时应与旧函数一致：%+v vs %+v", legacy, with)
	}
}
