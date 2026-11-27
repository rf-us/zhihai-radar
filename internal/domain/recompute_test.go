package domain

import (
	"math"
	"testing"
	"time"
)

func TestRecomputeWithCostMatchesFullScore(t *testing.T) {
	// 用 Score 算一遍完整输入，再用落库分数 + 同一个 D5 重算，应一致。
	in := Input{
		Zhihu:     []Evidence{{Votes: 100, Comments: 50, Authority: 3}},
		Global:    []Evidence{{URL: "https://a.com/x"}},
		Pains:     []Pain{{Severity: 4, EvidenceURL: "https://www.zhihu.com/q/1"}},
		CostLines: []CostLine{{Qty: 2, UnitPrice: 40, LossRate: 5}},
		Now:       time.Now(),
	}
	full, _ := Score(in)
	if full.Cost == nil {
		t.Fatal("样例应有 D5")
	}
	re, verdict := RecomputeWithCost(
		Scores{Demand: full.Demand, Pain: full.Pain, Trend: full.Trend, Competition: full.Competition}, full.Cost)
	if math.Abs(re.Overall-full.Overall) > 1e-9 || math.Abs(re.Confidence-full.Confidence) > 1e-9 {
		t.Fatalf("重算与全量不一致：%+v vs %+v", re, full)
	}
	if verdict != Verdict(full, full.Cost != nil) {
		t.Fatalf("结论不一致：%s vs %s", verdict, Verdict(full, full.Cost != nil))
	}
}

func TestRecomputeWithCostCanBeRemoved(t *testing.T) {
	base := Scores{Demand: 90, Pain: 80, Trend: 70, Competition: 20, Confidence: 1}
	cost := 100.0
	with, verdict := RecomputeWithCost(base, &cost)
	if with.Confidence != 1 || verdict != "值得验证" {
		t.Fatalf("有成本时应判值得验证：%+v %s", with, verdict)
	}
	without, verdict2 := RecomputeWithCost(with, nil)
	if math.Abs(without.Confidence-0.8) > 1e-9 {
		t.Fatalf("删成本后应回到 0.8：%+v", without)
	}
	if verdict2 != "需进一步验证" {
		t.Fatalf("删成本后不应判值得验证：%s", verdict2)
	}
}
