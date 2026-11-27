package domain

import (
	"fmt"
	"math"
)

// CostExtras 是 BOM 之外的成本项。
type CostExtras struct {
	Packaging      float64 `json:"packaging"`       // 包装，直接加到单台成本
	Freight        float64 `json:"freight"`         // 头程运费，直接加到单台成本
	CommissionRate float64 `json:"commission_rate"` // 平台佣金率，百分数（15 表示 15%），从建议售价里扣
}

func (e CostExtras) Validate() error {
	if e.Packaging < 0 {
		return fmt.Errorf("packaging 不能为负")
	}
	if e.Freight < 0 {
		return fmt.Errorf("freight 不能为负")
	}
	if e.CommissionRate < 0 || e.CommissionRate > 100 {
		return fmt.Errorf("commission_rate 必须在 0–100 之间（当前 %.2f）", e.CommissionRate)
	}
	return nil
}

// EstimateCostWithExtras 在 EstimateCost 基础上计入包装、头程与平台佣金。
//
// 佣金的处理：建议售价是"用户到手价"，平台抽走 commission_rate% 后才是收入；
// 因此毛利 = (售价×(1-佣金率) - 总成本) / 售价。这样 15% 佣金会真实反映在毛利与 D5。
//
// 没有附加项时结果必须与 EstimateCost 完全一致（保留旧函数给已有调用方）。
func EstimateCostWithExtras(lines []CostLine, target *float64, prices []float64, extras CostExtras) (CostBreakdown, bool) {
	if err := extras.Validate(); err != nil {
		return CostBreakdown{}, false
	}
	cb, ok := EstimateCost(lines, target, prices)
	if !ok {
		return cb, false
	}
	if extras.Packaging == 0 && extras.Freight == 0 && extras.CommissionRate == 0 {
		return cb, true
	}

	total := cb.TotalCost + extras.Packaging + extras.Freight
	cb.TotalCost = round2(total)
	// 建议售价来源不变：手工/竞品均价×0.8/成本反推；成本反推时用含附加项的总成本。
	if cb.PriceSource == "成本反推（保证 40% 毛利）" {
		cb.Suggested = round2(total / CostReverseMargin)
	}
	if cb.Suggested <= 0 {
		return CostBreakdown{}, false
	}
	net := cb.Suggested * (1 - extras.CommissionRate/100)
	cb.Margin = round4((net - total) / cb.Suggested)
	if cb.Margin >= MarginFullScore {
		cb.Score = 100
	} else {
		cb.Score = round2(clamp(cb.Margin/MarginFullScore*100, 0, 100))
	}
	return cb, true
}

func round4(v float64) float64 {
	return math.Round(v*10000) / 10000
}
