// Package supply 实现供应链比价的纯逻辑。
//
// 金额用 float64 存（与 internal/domain 一致），但比价一律先转成整数分再排序，
// 避免浮点误差让"看起来更便宜的"排到后面。
package supply

import (
	"errors"
	"fmt"
	"math"
	"sort"
)

// Tier 是一个阶梯价档位：数量 ≥ MinQty 时单价为 UnitPrice。
type Tier struct {
	MinQty    int     `json:"min_qty"`
	UnitPrice float64 `json:"unit_price"`
}

// Supplier 是一家供应商。
type Supplier struct {
	ID           int64  `json:"id"`
	Name         string `json:"name"`
	Region       string `json:"region"`
	Contact      string `json:"contact"`
	PaymentTerms string `json:"payment_terms"`
	Note         string `json:"note"`
}

// Quote 是一条报价。
type Quote struct {
	ID            int64   `json:"id"`
	OpportunityID int64   `json:"opportunity_id"`
	SupplierID    int64   `json:"supplier_id"`
	SupplierName  string  `json:"supplier_name,omitempty"`
	PartName      string  `json:"part_name"`
	UnitPrice     float64 `json:"unit_price"`
	Currency      string  `json:"currency"`
	MOQ           int     `json:"moq"`
	LeadTimeDays  int     `json:"lead_time_days"`
	Tiers         []Tier  `json:"tiers"`
	Selected      bool    `json:"selected"`
}

// Comparison 是一条比价结果。
type Comparison struct {
	QuoteID      int64   `json:"quote_id"`
	SupplierID   int64   `json:"supplier_id"`
	SupplierName string  `json:"supplier_name"`
	UnitPrice    float64 `json:"unit_price"`  // 命中档位后的单价
	TotalCents   int64   `json:"total_cents"` // 整数分，用于排序
	Total        float64 `json:"total"`
	Currency     string  `json:"currency"`
	LeadTimeDays int     `json:"lead_time_days"`
	MOQ          int     `json:"moq"`
	MatchedTier  *int    `json:"matched_tier,omitempty"`
	Reason       string  `json:"reason"`
	Recommended  bool    `json:"recommended"`
}

// Validate 校验报价字段；返回的错误信息可直接回给前端就地提示。
func (q Quote) Validate() error {
	if q.UnitPrice <= 0 {
		return errors.New("unit_price 必须大于 0")
	}
	if q.MOQ < 1 {
		return errors.New("moq 必须 ≥ 1")
	}
	if q.LeadTimeDays < 0 {
		return errors.New("lead_time_days 不能为负")
	}
	if q.Currency != "CNY" && q.Currency != "USD" {
		return errors.New("currency 只支持 CNY 或 USD")
	}
	for i, t := range q.Tiers {
		if t.MinQty < 1 {
			return fmt.Errorf("tiers[%d].min_qty 必须 ≥ 1", i)
		}
		if t.UnitPrice <= 0 {
			return fmt.Errorf("tiers[%d].unit_price 必须大于 0", i)
		}
	}
	return nil
}

// unitPriceAt 取满足 qty 的"最低档单价"（issue #4 口径）：数量越大单价越低，
// 因此取 MinQty<=qty 中 MinQty 最大的那档（命中数量门槛最高），没有命中则
// 用报价单价。返回的 *int 是原始 tiers 切片里的下标。
func unitPriceAt(q Quote, qty int) (float64, *int) {
	if len(q.Tiers) == 0 {
		return q.UnitPrice, nil
	}
	best := -1
	for i, t := range q.Tiers {
		if qty >= t.MinQty && (best < 0 || t.MinQty > q.Tiers[best].MinQty) {
			best = i
		}
	}
	if best < 0 {
		return q.UnitPrice, nil
	}
	return q.Tiers[best].UnitPrice, &best
}

// toCents 把金额转成整数分，用四舍五入避免 0.1+0.2 这类误差。
func toCents(amount float64) int64 {
	return int64(math.Round(amount * 100))
}

// Compare 按目标数量计算每条报价的总价并排序：总价升序 → 交期升序 → MOQ 升序。
// 返回的第一条即推荐项（Reason 说明为什么推荐）。
//
// 只做只读计算，不落库；落库只发生在选定动作。
func Compare(quotes []Quote, qty int) ([]Comparison, error) {
	if qty <= 0 {
		return nil, errors.New("qty 必须大于 0")
	}
	out := make([]Comparison, 0, len(quotes))
	for _, q := range quotes {
		if err := q.Validate(); err != nil {
			return nil, fmt.Errorf("报价 %d 不合法：%w", q.ID, err)
		}
		// MOQ 约束：目标数量低于 MOQ 时按 MOQ 计算，否则按 qty。
		effectiveQty := qty
		if effectiveQty < q.MOQ {
			effectiveQty = q.MOQ
		}
		unit, tier := unitPriceAt(q, effectiveQty)
		total := unit * float64(effectiveQty)
		cents := toCents(total)
		c := Comparison{
			QuoteID: q.ID, SupplierID: q.SupplierID, SupplierName: q.SupplierName,
			UnitPrice: unit, TotalCents: cents, Total: float64(cents) / 100,
			Currency: q.Currency, LeadTimeDays: q.LeadTimeDays, MOQ: q.MOQ,
			MatchedTier: tier,
		}
		out = append(out, c)
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].TotalCents != out[j].TotalCents {
			return out[i].TotalCents < out[j].TotalCents
		}
		if out[i].LeadTimeDays != out[j].LeadTimeDays {
			return out[i].LeadTimeDays < out[j].LeadTimeDays
		}
		return out[i].MOQ < out[j].MOQ
	})

	if len(out) > 0 {
		out[0].Recommended = true
		out[0].Reason = fmt.Sprintf("总价最低（%s %.2f）且交期 %d 天", out[0].Currency, out[0].Total, out[0].LeadTimeDays)
		for i := 1; i < len(out); i++ {
			if out[i].TotalCents == out[0].TotalCents {
				out[i].Reason = "与推荐项同价，交期或 MOQ 略逊"
			} else {
				out[i].Reason = fmt.Sprintf("比推荐项贵 %.2f %s", float64(out[i].TotalCents-out[0].TotalCents)/100, out[i].Currency)
			}
		}
	}
	return out, nil
}
