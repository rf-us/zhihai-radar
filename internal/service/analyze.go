package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/rf-us/zhihai-radar/internal/domain"
	"github.com/rf-us/zhihai-radar/internal/source"
	"github.com/rf-us/zhihai-radar/internal/store"
)

// Analyzer 编排一次选品分析：取数 → 评分 → 落库。
type Analyzer struct {
	src source.Source
	st  *store.Store
	now func() time.Time
}

func NewAnalyzer(src source.Source, st *store.Store) *Analyzer {
	return &Analyzer{src: src, st: st, now: time.Now}
}

type AnalyzeInput struct {
	Keyword     string
	Category    string
	TargetPrice *float64
	Operator    string
}

type Detail struct {
	Opportunity    store.Opportunity `json:"opportunity"`
	Evidence       []store.Evidence  `json:"evidence"`
	Reasons        []string          `json:"reasons"`
	Degraded       bool              `json:"degraded"`
	DegradedReason string            `json:"degraded_reason,omitempty"`
}

const (
	callTimeout = 12 * time.Second
	maxEvidence = 30
)

func (a *Analyzer) Analyze(ctx context.Context, in AnalyzeInput) (*Detail, error) {
	keyword := strings.TrimSpace(in.Keyword)
	if n := utf8.RuneCountInString(keyword); n == 0 || n > 40 {
		return nil, errors.New("品类词长度需在 1–40 个字符之间")
	}

	var (
		zhihu, global, hot          []source.Post
		errZhihu, errGlobal, errHot error
	)
	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		zhihu, errZhihu = a.call(ctx, func(c context.Context) ([]source.Post, error) {
			return a.src.SearchZhihu(c, keyword, 10)
		})
	}()
	go func() {
		defer wg.Done()
		global, errGlobal = a.call(ctx, func(c context.Context) ([]source.Post, error) {
			return a.src.SearchGlobal(c, keyword, 20)
		})
	}()
	go func() {
		defer wg.Done()
		hot, errHot = a.call(ctx, func(c context.Context) ([]source.Post, error) {
			return a.src.Hot(c, 30)
		})
	}()
	wg.Wait()

	hotHit := false
	for _, p := range hot {
		if strings.Contains(p.Title, keyword) || strings.Contains(p.Snippet, keyword) {
			hotHit = true
			break
		}
	}

	// 痛点由直答生成；fixture 模式直接取快照里的答案。
	answer := ""
	if f, ok := a.src.(*source.Fixture); ok {
		answer = f.AnswerFor(keyword)
	} else {
		var err error
		answer, err = a.callText(ctx, buildPrompt(keyword, zhihu))
		if err != nil {
			errZhihu = errors.Join(errZhihu, fmt.Errorf("直答: %w", err))
		}
	}
	pains := parsePains(answer, urlSet(zhihu, global))

	input := domain.Input{
		Keyword:     keyword,
		Zhihu:       toEvidence(zhihu),
		Global:      toEvidence(global),
		HotHit:      hotHit,
		Pains:       pains,
		TargetPrice: in.TargetPrice,
		Now:         a.now(),
	}
	scores, reasons := domain.Score(input)
	verdict := domain.Verdict(scores, scores.Cost != nil)

	opp := store.Opportunity{
		Keyword:  keyword,
		Name:     keyword,
		Category: in.Category,
		Scores:   scores,
		Verdict:  verdict,
		Reasons:  reasons,
		PainJSON: painJSON(pains),
		Status:   "analyzed",
	}
	if err := a.st.CreateOpportunity(ctx, &opp); err != nil {
		return nil, err
	}

	evidence := mergeEvidence(zhihu, global, hot, maxEvidence)
	if err := a.st.SaveEvidence(ctx, opp.ID, evidence); err != nil {
		return nil, err
	}

	out := &Detail{Opportunity: opp, Evidence: evidence, Reasons: reasons}
	if msg := degradedMessage(errZhihu, errGlobal, errHot); msg != "" {
		out.Degraded = true
		out.DegradedReason = msg
	}
	return out, nil
}

func (a *Analyzer) Get(ctx context.Context, id int64) (*Detail, error) {
	opp, err := a.st.GetOpportunity(ctx, id)
	if err != nil {
		return nil, err
	}
	evidence, err := a.st.ListEvidence(ctx, id)
	if err != nil {
		return nil, err
	}
	return &Detail{Opportunity: *opp, Evidence: evidence, Reasons: opp.Reasons}, nil
}

func (a *Analyzer) List(ctx context.Context, limit int) ([]store.Opportunity, error) {
	return a.st.ListOpportunities(ctx, limit)
}

func (a *Analyzer) call(ctx context.Context, fn func(context.Context) ([]source.Post, error)) ([]source.Post, error) {
	c, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	return fn(c)
}

func (a *Analyzer) callText(ctx context.Context, prompt string) (string, error) {
	c, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	return a.src.Ask(c, prompt)
}

// buildPrompt 只喂 Top5 的标题与摘要：直答的输入越长越容易跑偏，而证据链
// 需要的只是"用户到底在抱怨什么"，不需要全文。
func buildPrompt(keyword string, posts []source.Post) string {
	var b strings.Builder
	fmt.Fprintf(&b, "下面是知乎上关于「%s」的讨论摘要。请总结用户在抱怨什么，", keyword)
	b.WriteString(`只输出 JSON：{"pain_points":[{"text":"...","severity":1-5,"evidence_url":"..."}]}。`)
	b.WriteString("severity 表示严重程度；evidence_url 必须原样取自下面出现过的链接，不要编造。\n\n")
	n := len(posts)
	if n > 5 {
		n = 5
	}
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "- %s\n  %s\n  %s\n", posts[i].Title, posts[i].Snippet, posts[i].URL)
	}
	return b.String()
}

type painPoint struct {
	Text        string `json:"text"`
	Severity    int    `json:"severity"`
	EvidenceURL string `json:"evidence_url"`
}

// parsePains 解析模型输出，并**只保留链接出现在本次检索结果里的痛点**。
//
// 这是防幻觉的关键：模型很擅长编一个看起来合理的知乎链接，而"看起来像链接"
// 用前缀检查挡不住，必须对照白名单。
func parsePains(raw string, allowed map[string]bool) []domain.Pain {
	body := extractJSON(raw)
	if body == "" {
		return nil
	}
	var payload struct {
		PainPoints []painPoint `json:"pain_points"`
	}
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		return nil
	}
	out := make([]domain.Pain, 0, len(payload.PainPoints))
	for _, p := range payload.PainPoints {
		if p.Severity < 1 || p.Severity > 5 {
			continue
		}
		if !allowed[canonicalURL(p.EvidenceURL)] {
			continue
		}
		text := strings.TrimSpace(p.Text)
		if text == "" {
			continue
		}
		out = append(out, domain.Pain{Text: text, Severity: p.Severity, EvidenceURL: p.EvidenceURL})
	}
	return out
}

// extractJSON 容忍模型把 JSON 包在 ``` 围栏里，或前后带一句解释。
func extractJSON(raw string) string {
	s := strings.TrimSpace(raw)
	if strings.HasPrefix(s, "```") {
		if i := strings.IndexByte(s, '\n'); i >= 0 {
			s = s[i+1:]
		}
		if j := strings.LastIndex(s, "```"); j >= 0 {
			s = s[:j]
		}
	}
	start := strings.IndexByte(s, '{')
	end := strings.LastIndexByte(s, '}')
	if start < 0 || end <= start {
		return ""
	}
	return s[start : end+1]
}

func urlSet(groups ...[]source.Post) map[string]bool {
	out := map[string]bool{}
	for _, g := range groups {
		for _, p := range g {
			if u := canonicalURL(p.URL); u != "" {
				out[u] = true
			}
		}
	}
	return out
}

func canonicalURL(raw string) string {
	u := strings.TrimSpace(raw)
	u = strings.TrimPrefix(u, "http://")
	u = strings.TrimPrefix(u, "https://")
	u = strings.TrimSuffix(u, "/")
	return strings.ToLower(u)
}

func toEvidence(posts []source.Post) []domain.Evidence {
	out := make([]domain.Evidence, 0, len(posts))
	for _, p := range posts {
		out = append(out, domain.Evidence{
			Source: p.Source, Title: p.Title, Snippet: p.Snippet, URL: p.URL,
			Votes: p.Votes, Comments: p.Comments, Authority: p.Authority,
			PublishedAt: p.PublishedAt,
		})
	}
	return out
}

func mergeEvidence(zhihu, global, hot []source.Post, max int) []store.Evidence {
	var out []store.Evidence
	seen := map[string]bool{}
	add := func(posts []source.Post) {
		for _, p := range posts {
			u := canonicalURL(p.URL)
			if u == "" || seen[u] {
				continue
			}
			seen[u] = true
			out = append(out, store.Evidence{
				Source: p.Source, Title: p.Title, Snippet: p.Snippet, URL: p.URL,
				Votes: p.Votes, Comments: p.Comments, Authority: p.Authority,
				PublishedAt: p.PublishedAt, Rank: len(out),
			})
			if len(out) >= max {
				return
			}
		}
	}
	add(zhihu)
	if len(out) < max {
		add(global)
	}
	if len(out) < max {
		add(hot)
	}
	return out
}

func painJSON(pains []domain.Pain) string {
	if len(pains) == 0 {
		return "[]"
	}
	raw, err := json.Marshal(pains)
	if err != nil {
		return "[]"
	}
	return string(raw)
}

// degradedMessage 把三路失败归纳成一句给用户看的话。
func degradedMessage(errs ...error) string {
	var parts []string
	labels := []string{"知乎搜索", "全网搜索", "热榜"}
	for i, err := range errs {
		if err == nil {
			continue
		}
		switch {
		case errors.Is(err, source.ErrQuotaExhausted):
			parts = append(parts, labels[i]+"配额耗尽")
		case errors.Is(err, source.ErrRateLimited):
			parts = append(parts, labels[i]+"被限流")
		default:
			parts = append(parts, labels[i]+"不可用")
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "；") + "，本次分数按可用维度归一化"
}
