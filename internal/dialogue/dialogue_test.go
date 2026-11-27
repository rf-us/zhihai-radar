package dialogue

import (
	"errors"
	"testing"
)

func validAnswers() Answers {
	return Answers{
		"direction": "coffee",
		"market":    "na",
		"price":     "usd_80_150",
		"supply":    "oem",
		"concerns":  "certification,logistics,competition",
		"depth":     "full",
	}
}

func TestDeriveBuildsPlan(t *testing.T) {
	p, err := Derive(validAnswers())
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	if p.Keyword != "便携咖啡机" || p.Category != "家居小家电" {
		t.Fatalf("关键词与品类推导错误: %+v", p)
	}
	if p.TargetUSD == nil || *p.TargetUSD != 115 {
		t.Fatalf("价格区间应取中值 115，得到 %v", p.TargetUSD)
	}
	if p.TargetCNY == nil || *p.TargetCNY != 115*USDToCNY {
		t.Fatalf("人民币换算错误: %v", p.TargetCNY)
	}
	if p.EvidenceMax != 30 {
		t.Fatalf("full 深度应保留 30 条证据，得到 %d", p.EvidenceMax)
	}
	if len(p.Sections) != 2 || p.Sections[0] != "compliance" || p.Sections[1] != "logistics" {
		t.Fatalf("条件章节应由顾虑触发且顺序稳定，得到 %v", p.Sections)
	}
}

func TestQuickDepthLimitsEvidence(t *testing.T) {
	a := validAnswers()
	a["depth"] = "quick"
	p, err := Derive(a)
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	if p.EvidenceMax != 8 {
		t.Fatalf("快速模式应只留 8 条证据，得到 %d", p.EvidenceMax)
	}
}

func TestFreeTextDirection(t *testing.T) {
	a := validAnswers()
	a["direction"] = "  折叠电动滑板车  "
	p, err := Derive(a)
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	if p.Keyword != "折叠电动滑板车" {
		t.Fatalf("自填应作为关键词并且去空格，得到 %q", p.Keyword)
	}
	if p.Category != "" {
		t.Fatalf("自填时品类应为空，得到 %q", p.Category)
	}
}

func TestMissingRequired(t *testing.T) {
	for _, key := range []string{"direction", "market", "price", "supply"} {
		a := validAnswers()
		delete(a, key)
		if _, err := Derive(a); !errors.Is(err, ErrMissing) {
			t.Fatalf("缺少 %s 时应返回 ErrMissing，得到 %v", key, err)
		}
	}
}

func TestUnknownPriceOption(t *testing.T) {
	a := validAnswers()
	a["price"] = "usd_999"
	if _, err := Derive(a); err == nil {
		t.Fatal("未知价格区间应报错，不能静默当成免费")
	}
}

func TestNoConcernsMeansNoExtraSections(t *testing.T) {
	a := validAnswers()
	delete(a, "concerns")
	p, err := Derive(a)
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	if len(p.Sections) != 0 {
		t.Fatalf("没选顾虑就不该多出章节，得到 %v", p.Sections)
	}
}

func TestQuestionsAreStable(t *testing.T) {
	qs := Questions()
	if len(qs) != 6 {
		t.Fatalf("题库应为 6 题，得到 %d", len(qs))
	}
	keys := []string{"direction", "market", "price", "supply", "concerns", "depth"}
	for i, want := range keys {
		if qs[i].Key != want {
			t.Fatalf("第 %d 题应为 %s，得到 %s（顺序变了会影响报告章节触发）", i+1, want, qs[i].Key)
		}
	}
}
