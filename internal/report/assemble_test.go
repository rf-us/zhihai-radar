package report

import (
	"strings"
	"testing"
	"time"

	"github.com/rf-us/zhihai-radar/internal/domain"
	"github.com/rf-us/zhihai-radar/internal/store"
)

func TestAssembleFixedAndConditionalSections(t *testing.T) {
	now := time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)
	opp := store.Opportunity{
		Keyword: "便携咖啡机", Name: "便携咖啡机", Category: "北美",
		Scores:  domain.Scores{Demand: 80, Pain: 70, Trend: 60, Competition: 30, Confidence: 0.8},
		Verdict: "需进一步验证", Reasons: []string{"D5 未测算：综合分只覆盖 0.80 的权重"},
		CreatedAt: now,
	}
	doc := Assemble(AssembleInput{Opportunity: opp, Now: now})
	if len(doc.Sections) != 6 {
		t.Fatalf("无条件章节应为 6 节，got %d", len(doc.Sections))
	}
	// 无成本时必须说"未测算"，不能编 0。
	costSection := doc.Sections[3]
	if !strings.Contains(costSection.Lead, "未测算") {
		t.Fatalf("缺成本应写未测算：%s", costSection.Lead)
	}

	withCond := Assemble(AssembleInput{Opportunity: opp, Now: now, Sections: []string{"compliance", "logistics"}})
	if len(withCond.Sections) != 8 {
		t.Fatalf("两个条件章节应为 8 节，got %d", len(withCond.Sections))
	}
	if withCond.Sections[6].Key != "compliance" || withCond.Sections[7].Key != "logistics" {
		t.Fatalf("条件章节顺序错误：%+v", withCond.Sections[6:])
	}
}

func TestAssembleEvidenceIsTraceable(t *testing.T) {
	now := time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)
	pub := now.AddDate(0, 0, -5)
	opp := store.Opportunity{Keyword: "x", Name: "x", CreatedAt: now,
		Scores: domain.Scores{Demand: 80, Confidence: 0.8}}
	doc := Assemble(AssembleInput{
		Opportunity: opp, Now: now,
		Evidence: []store.Evidence{{
			Source: "zhihu_search", Title: "真实体验", URL: "https://www.zhihu.com/q/1",
			Snippet: "清洗麻烦", PublishedAt: &pub,
		}},
	})
	if len(doc.Sections[1].Evidence) != 1 {
		t.Fatal("应带一条证据")
	}
	ev := doc.Sections[1].Evidence[0]
	if ev.URL != "https://www.zhihu.com/q/1" || ev.CollectedAt != "2026-11-22" {
		t.Fatalf("证据不可回溯：%+v", ev)
	}
}

func TestAssembleIsDeterministic(t *testing.T) {
	now := time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)
	opp := store.Opportunity{Keyword: "x", Name: "x", CreatedAt: now,
		Scores: domain.Scores{Demand: 80, Confidence: 0.8}}
	a := Assemble(AssembleInput{Opportunity: opp, Now: now})
	b := Assemble(AssembleInput{Opportunity: opp, Now: now})
	if a.Meta.GeneratedAt != b.Meta.GeneratedAt {
		t.Fatal("注入同一时间应生成同一时间戳")
	}
}
