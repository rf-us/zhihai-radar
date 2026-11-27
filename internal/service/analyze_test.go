package service

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rf-us/zhihai-radar/internal/source"
	"github.com/rf-us/zhihai-radar/internal/store"
)

type stubSource struct {
	zhihu, global, hot []source.Post
	answer             string
	zhihuErr           error
}

func (s *stubSource) SearchZhihu(context.Context, string, int) ([]source.Post, error) {
	return s.zhihu, s.zhihuErr
}
func (s *stubSource) SearchGlobal(context.Context, string, int) ([]source.Post, error) {
	return s.global, nil
}
func (s *stubSource) Hot(context.Context, int) ([]source.Post, error) { return s.hot, nil }
func (s *stubSource) Ask(context.Context, string) (string, error)     { return s.answer, nil }

func newAnalyzer(t *testing.T, src source.Source) *Analyzer {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "radar.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	a := NewAnalyzer(src, st)
	a.now = func() time.Time { return time.Date(2026, 11, 27, 0, 0, 0, 0, time.UTC) }
	return a
}

func TestAnalyzePersistsAndScores(t *testing.T) {
	now := time.Now().UTC()
	recent := now.AddDate(0, 0, -3)
	src := &stubSource{
		zhihu: []source.Post{
			{Title: "有哪些买了不后悔的小家电", URL: "https://www.zhihu.com/question/1", Votes: 500, Comments: 60, Authority: 4, PublishedAt: &recent},
			{Title: "便携咖啡机值得买吗", URL: "https://www.zhihu.com/question/2", Votes: 300, Comments: 40, Authority: 3},
		},
		global: []source.Post{
			{Title: "Best portable espresso 2026", URL: "https://example.com/a", PublishedAt: &recent},
			{Title: "便携咖啡机横评", URL: "https://example.com/b", PublishedAt: &recent},
		},
		hot: []source.Post{
			{Title: "便携咖啡机为什么火了", URL: "https://www.zhihu.com/hot/1"},
		},
		answer: "```json\n{\"pain_points\":[" +
			"{\"text\":\"清洗麻烦\",\"severity\":5,\"evidence_url\":\"https://www.zhihu.com/question/1\"}," +
			"{\"text\":\"漏水\",\"severity\":4,\"evidence_url\":\"https://www.zhihu.com/question/2\"}," +
			"{\"text\":\"编造的痛点\",\"severity\":5,\"evidence_url\":\"https://fake.example/x\"}" +
			"]}\n```",
	}
	a := newAnalyzer(t, src)

	detail, err := a.Analyze(context.Background(), AnalyzeInput{Keyword: "便携咖啡机", Category: "家居小家电"})
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	if detail.Opportunity.ID == 0 {
		t.Fatal("应回填 ID")
	}
	if detail.Opportunity.Verdict == "" {
		t.Fatal("必须给结论")
	}
	// 编造的链接必须被丢弃，只留两个真实痛点。
	if !strings.Contains(detail.Opportunity.PainJSON, "清洗麻烦") {
		t.Fatalf("阈值内的痛点丢失: %s", detail.Opportunity.PainJSON)
	}
	if strings.Contains(detail.Opportunity.PainJSON, "编造的痛点") {
		t.Fatal("不在检索结果里的链接必须被丢弃（防模型编造）")
	}
	if detail.Opportunity.Scores.Pain != 36 { // (5+4)*4
		t.Fatalf("D2 应为 36，得到 %v", detail.Opportunity.Scores.Pain)
	}
	if detail.Opportunity.Scores.Trend < 40 {
		t.Fatalf("热榜命中时 D3 应包含 40 分基础分，得到 %v", detail.Opportunity.Scores.Trend)
	}

	// 读回：证据应持久化且去重。
	got, err := a.Get(context.Background(), detail.Opportunity.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(got.Evidence) != 5 {
		t.Fatalf("证据应有 5 条（2 知乎 + 2 全网 + 1 热榜），得到 %d", len(got.Evidence))
	}
	for i := 1; i < len(got.Evidence); i++ {
		if got.Evidence[i].Rank <= got.Evidence[i-1].Rank {
			t.Fatalf("rank 应递增: %+v", got.Evidence)
		}
	}
}

func TestAnalyzeDegradesOnQuota(t *testing.T) {
	src := &stubSource{
		zhihuErr: source.ErrQuotaExhausted,
		global:   []source.Post{{Title: "g", URL: "https://example.com/g"}},
		answer:   `{"pain_points":[]}`,
	}
	a := newAnalyzer(t, src)

	detail, err := a.Analyze(context.Background(), AnalyzeInput{Keyword: "折叠露营灯"})
	if err != nil {
		t.Fatalf("单路失败不应让整次分析失败: %v", err)
	}
	if !detail.Degraded || !strings.Contains(detail.DegradedReason, "配额") {
		t.Fatalf("应标注降级与原因，得到 %q", detail.DegradedReason)
	}
	if detail.Opportunity.Scores.Confidence >= 1 {
		t.Fatalf("缺一维时置信度必须小于 1，得到 %v", detail.Opportunity.Scores.Confidence)
	}
}

func TestAnalyzeRejectsBadKeyword(t *testing.T) {
	a := newAnalyzer(t, &stubSource{})
	for _, kw := range []string{"", "   ", strings.Repeat("长", 41)} {
		if _, err := a.Analyze(context.Background(), AnalyzeInput{Keyword: kw}); err == nil {
			t.Fatalf("关键词 %q 应被拒绝", kw)
		}
	}
}

func TestParsePainsHandlesFencesAndBadJSON(t *testing.T) {
	allowed := map[string]bool{"www.zhihu.com/question/1": true}
	cases := []struct {
		name string
		raw  string
		want int
	}{
		{"围栏", "```json\n{\"pain_points\":[{\"text\":\"a\",\"severity\":3,\"evidence_url\":\"https://www.zhihu.com/question/1\"}]}\n```", 1},
		{"前后有话", "好的，结果如下：{\"pain_points\":[{\"text\":\"b\",\"severity\":2,\"evidence_url\":\"https://www.zhihu.com/question/1\"}]} 以上。", 1},
		{"非法 JSON", "抱歉我无法完成", 0},
		{"越界严重度", `{"pain_points":[{"text":"c","severity":9,"evidence_url":"https://www.zhihu.com/question/1"}]}`, 0},
	}
	for _, c := range cases {
		if got := parsePains(c.raw, allowed); len(got) != c.want {
			t.Errorf("%s: 期望 %d 条，得到 %d", c.name, c.want, len(got))
		}
	}
}

func TestAnalyzeUsesFixtureAnswer(t *testing.T) {
	f := source.NewFixture(source.Dataset{
		Keyword: "便携咖啡机",
		Zhihu:   []source.Post{{Title: "t", URL: "https://www.zhihu.com/q/1", Votes: 10}},
		Answer:  `{"pain_points":[{"text":"重","severity":3,"evidence_url":"https://www.zhihu.com/q/1"}]}`,
	})
	a := newAnalyzer(t, f)
	detail, err := a.Analyze(context.Background(), AnalyzeInput{Keyword: "便携咖啡机"})
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	if !strings.Contains(detail.Opportunity.PainJSON, "重") {
		t.Fatalf("fixture 答案未被采用: %s", detail.Opportunity.PainJSON)
	}
}
