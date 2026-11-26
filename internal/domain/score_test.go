package domain

import (
	"math"
	"testing"
	"time"
)

func post(title string, votes, comments int, authority float64, daysAgo int, now time.Time) Evidence {
	t := now.AddDate(0, 0, -daysAgo)
	return Evidence{
		Source: "global_search", Title: title, URL: "https://example.com/" + title,
		Votes: votes, Comments: comments, Authority: authority, PublishedAt: &t,
	}
}

func TestScoreEmptyInputGivesNoConfidence(t *testing.T) {
	s, reasons := Score(Input{Keyword: "测试"})
	if s.Overall != 0 || s.Confidence != 0 {
		t.Fatalf("无数据时应为 0，得到 %+v", s)
	}
	if got := Verdict(s, false); got != "不建议" {
		t.Fatalf("无数据应判不建议，得到 %q", got)
	}
	if len(reasons) == 0 {
		t.Fatal("即使无数据也要给解释")
	}
}

func TestScoreHighSignalIsWorthValidating(t *testing.T) {
	now := time.Date(2026, 11, 26, 0, 0, 0, 0, time.UTC)
	var zhihu []Evidence
	for i := 0; i < 10; i++ {
		zhihu = append(zhihu, post("知乎回答", 500, 50, 4, 10, now))
	}
	var global []Evidence
	for _, title := range []string{"竞品 A", "竞品 B", "竞品 C"} {
		global = append(global, post(title, 10, 2, 2, 5, now))
	}
	in := Input{
		Keyword: "便携咖啡机",
		Zhihu:   zhihu,
		Global:  global,
		HotHit:  true,
		Pains: []Pain{
			{Text: "清洗麻烦", Severity: 5, EvidenceURL: "https://zhihu.com/a"},
			{Text: "漏水", Severity: 5, EvidenceURL: "https://zhihu.com/b"},
			{Text: "太重", Severity: 5, EvidenceURL: "https://zhihu.com/c"},
			{Text: "噪音大", Severity: 5, EvidenceURL: "https://zhihu.com/d"},
			{Text: "贵", Severity: 5, EvidenceURL: "https://zhihu.com/e"},
		},
		Prices:      []float64{100, 120},
		CostLines:   []CostLine{{Qty: 1, UnitPrice: 60}},
		TargetPrice: f64(159),
		Now:         now,
	}
	s, _ := Score(in)
	if s.Demand != 100 || s.Pain != 100 || s.Trend != 100 {
		t.Fatalf("高信号三维应满分，得到 %+v", s)
	}
	if s.Cost == nil || *s.Cost != 100 {
		t.Fatalf("毛利 62%% 时 D5 应满分，得到 %v", *s.Cost)
	}
	if s.Overall < 90 || s.Confidence != 1 {
		t.Fatalf("综合分与置信度不对: %+v", s)
	}
	if got := Verdict(s, true); got != "值得验证" {
		t.Fatalf("应判值得验证，得到 %q", got)
	}
}

func TestScoreWithoutCostNeverWorthValidating(t *testing.T) {
	now := time.Date(2026, 11, 26, 0, 0, 0, 0, time.UTC)
	in := Input{
		Zhihu:  []Evidence{post("a", 500, 50, 4, 1, now)},
		Global: []Evidence{post("b", 10, 1, 2, 1, now)},
		HotHit: true,
		Pains:  []Pain{{Text: "x", Severity: 5, EvidenceURL: "https://z.com/1"}},
		Now:    now,
	}
	s, _ := Score(in)
	if s.Cost != nil {
		t.Fatal("没有 BOM 时 D5 必须是 nil，不能用 0 冒充")
	}
	if math.Abs(s.Confidence-0.8) > 1e-9 {
		t.Fatalf("可用权重应为 0.8，得到 %v", s.Confidence)
	}
	if got := Verdict(s, false); got == "值得验证" {
		t.Fatal("没有成本数据不得判值得验证")
	}
}

func TestEstimateCostHandlesUseMode(t *testing.T) {
	// use_mode=1：每 1000 台用 1 个，单价 0.5 → 单台 0.0005
	cb, ok := EstimateCost([]CostLine{{Qty: 1000, UnitPrice: 0.5, UseMode: 1}}, nil, nil)
	if !ok {
		t.Fatal("应能算出成本")
	}
	if math.Abs(cb.TotalCost-0.0005) > 1e-9 {
		t.Fatalf("use_mode=1 的单台成本应为 0.0005，得到 %v（写成 Qty×单价会虚高 1000 倍）", cb.TotalCost)
	}

	// 同样的数量按 use_mode=0 算，应该是 500
	cb2, _ := EstimateCost([]CostLine{{Qty: 1000, UnitPrice: 0.5, UseMode: 0}}, nil, nil)
	if math.Abs(cb2.TotalCost-500) > 1e-6 {
		t.Fatalf("use_mode=0 应为 500，得到 %v", cb2.TotalCost)
	}
}

func TestEstimateCostLossRateAndTarget(t *testing.T) {
	target := 159.0
	cb, ok := EstimateCost([]CostLine{{Qty: 2, UnitPrice: 40, LossRate: 5}}, &target, nil)
	if !ok {
		t.Fatal("应能算出成本")
	}
	if math.Abs(cb.TotalCost-84) > 1e-6 {
		t.Fatalf("2×40×1.05 = 84，得到 %v", cb.TotalCost)
	}
	if cb.Suggested != 159 || cb.PriceSource != "手工指定" {
		t.Fatalf("应使用手工售价，得到 %+v", cb)
	}
	if math.Abs(cb.Margin-(159.0-84.0)/159.0) > 1e-9 {
		t.Fatalf("毛利计算错误: %v", cb.Margin)
	}
}

func TestCompetitionCanVeto(t *testing.T) {
	now := time.Date(2026, 11, 26, 0, 0, 0, 0, time.UTC)
	var global []Evidence
	for i := 0; i < 12; i++ {
		global = append(global, post(string(rune('A'+i)), 1, 0, 1, 2, now))
	}
	s, _ := Score(Input{
		Zhihu:  []Evidence{post("a", 900, 90, 4, 1, now)},
		Global: global,
		Pains:  []Pain{{Text: "x", Severity: 5, EvidenceURL: "https://z.com/1"}},
		Now:    now,
	})
	if s.Competition < 80 {
		t.Fatalf("12 个竞品时 D4 应 >= 80，得到 %v", s.Competition)
	}
	if got := Verdict(s, false); got != "不建议" {
		t.Fatalf("竞争过度应一票否决，得到 %q", got)
	}
}

func TestPainWithoutEvidenceURLDoesNotCount(t *testing.T) {
	pains := []Pain{
		{Text: "有链接", Severity: 5, EvidenceURL: "https://zhihu.com/a"},
		{Text: "没链接", Severity: 5, EvidenceURL: ""},
		{Text: "假链接", Severity: 5, EvidenceURL: "javascript:alert(1)"},
	}
	score := painScore(pains)
	if score != 20 {
		t.Fatalf("只有 1 个有效痛点（severity 5 → 20 分），得到 %v", score)
	}
}

func f64(v float64) *float64 { return &v }
