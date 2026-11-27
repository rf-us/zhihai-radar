package source

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Zhihu 是知乎数据开放平台的真实 HTTP 客户端。
//
// 四个接口（核验于官方 Skill 包的 http-api.md）：
//
//	GET  /api/v1/content/zhihu_search?Query=&Count=
//	GET  /api/v1/content/global_search?Query=&Count=&Filter=&SearchDB=
//	GET  /api/v1/content/hot_list
//	POST /v1/chat/completions        （直答，OpenAI 兼容）
//
// 鉴权统一为 Authorization: Bearer <Access Secret> + X-Request-Timestamp（秒级）。
type Zhihu struct {
	base   string
	secret string
	hc     *http.Client
	now    func() time.Time
}

func NewZhihu(base, secret string) *Zhihu {
	if base == "" {
		base = "https://developer.zhihu.com"
	}
	return &Zhihu{
		base:   strings.TrimRight(base, "/"),
		secret: secret,
		hc:     &http.Client{Timeout: 20 * time.Second},
		now:    time.Now,
	}
}

const (
	pathZhihuSearch  = "/api/v1/content/zhihu_search"
	pathGlobalSearch = "/api/v1/content/global_search"
	pathHotList      = "/api/v1/content/hot_list"
	pathChat         = "/v1/chat/completions"
)

// envelope 是三个内容接口的统一外壳；Code 非 0 表示业务错误。
type envelope struct {
	Code    int             `json:"Code"`
	Message string          `json:"Message"`
	Data    json.RawMessage `json:"Data"`
}

type searchData struct {
	HasMore bool `json:"HasMore"`
	Items   []struct {
		Title          string `json:"Title"`
		ContentText    string `json:"ContentText"`
		URL            string `json:"Url"`
		CommentCount   int    `json:"CommentCount"`
		VoteUpCount    int    `json:"VoteUpCount"`
		AuthorityLevel string `json:"AuthorityLevel"`
		EditTime       int64  `json:"EditTime"`
	} `json:"Items"`
}

type hotData struct {
	Total int `json:"Total"`
	Items []struct {
		Title   string `json:"Title"`
		URL     string `json:"Url"`
		Summary string `json:"Summary"`
	} `json:"Items"`
}

func (z *Zhihu) SearchZhihu(ctx context.Context, keyword string, count int) ([]Post, error) {
	q := url.Values{}
	q.Set("Query", keyword)
	q.Set("Count", strconv.Itoa(clampCount(count, 10)))
	raw, err := z.get(ctx, pathZhihuSearch, q)
	if err != nil {
		return nil, err
	}
	return z.parseSearch(raw, "zhihu_search")
}

func (z *Zhihu) SearchGlobal(ctx context.Context, keyword string, count int) ([]Post, error) {
	q := url.Values{}
	q.Set("Query", keyword)
	q.Set("Count", strconv.Itoa(clampCount(count, 20)))
	raw, err := z.get(ctx, pathGlobalSearch, q)
	if err != nil {
		return nil, err
	}
	return z.parseSearch(raw, "global_search")
}

func (z *Zhihu) Hot(ctx context.Context, limit int) ([]Post, error) {
	raw, err := z.get(ctx, pathHotList, nil)
	if err != nil {
		return nil, err
	}
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("%w: 热榜响应无法解析", ErrUnavailable)
	}
	if env.Code != 0 {
		return nil, codeError(env.Code, env.Message)
	}
	var data hotData
	if err := json.Unmarshal(env.Data, &data); err != nil {
		return nil, fmt.Errorf("%w: 热榜数据结构不符", ErrUnavailable)
	}
	out := make([]Post, 0, len(data.Items))
	for i, it := range data.Items {
		if limit > 0 && i >= limit {
			break
		}
		out = append(out, Post{Source: "hot", Title: it.Title, Snippet: it.Summary, URL: it.URL, Authority: 1})
	}
	return out, nil
}

func (z *Zhihu) Ask(ctx context.Context, prompt string) (string, error) {
	body := map[string]any{
		"model":  "zhida-fast-1p5",
		"stream": false,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	}
	raw, err := z.post(ctx, pathChat, body)
	if err != nil {
		return "", err
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &out); err != nil || len(out.Choices) == 0 {
		return "", fmt.Errorf("%w: 直答响应结构不符", ErrUnavailable)
	}
	return out.Choices[0].Message.Content, nil
}

func (z *Zhihu) parseSearch(raw []byte, source string) ([]Post, error) {
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("%w: 搜索响应无法解析", ErrUnavailable)
	}
	if env.Code != 0 {
		return nil, codeError(env.Code, env.Message)
	}
	var data searchData
	if err := json.Unmarshal(env.Data, &data); err != nil {
		return nil, fmt.Errorf("%w: 搜索数据结构不符", ErrUnavailable)
	}
	out := make([]Post, 0, len(data.Items))
	for _, it := range data.Items {
		p := Post{
			Source: source, Title: it.Title, Snippet: it.ContentText, URL: it.URL,
			Votes: it.VoteUpCount, Comments: it.CommentCount,
			Authority: parseAuthority(it.AuthorityLevel),
		}
		if it.EditTime > 0 {
			t := time.Unix(it.EditTime, 0).UTC()
			p.PublishedAt = &t
		}
		out = append(out, p)
	}
	return out, nil
}

// parseAuthority 把字符串等级折成 0–4。
//
// 官方文档只说 AuthorityLevel 是 String，没给取值域；因此按"能解析成数字就用，
// 否则按关键字兜底，再不行按 0"处理——宁可区分度低，也不让整次分析失败。
func parseAuthority(s string) float64 {
	s = strings.TrimSpace(strings.ToLower(s))
	if v, err := strconv.ParseFloat(s, 64); err == nil {
		if v < 0 {
			return 0
		}
		if v > 4 {
			return 4
		}
		return v
	}
	switch {
	case strings.Contains(s, "high"), strings.Contains(s, "优"), strings.Contains(s, "高"):
		return 3
	case strings.Contains(s, "mid"), strings.Contains(s, "中"):
		return 2
	case s == "":
		return 0
	}
	return 1
}

func (z *Zhihu) get(ctx context.Context, path string, q url.Values) ([]byte, error) {
	u := z.base + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	return z.do(req)
}

func (z *Zhihu) post(ctx context.Context, path string, body any) ([]byte, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, z.base+path, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return z.do(req)
}

func (z *Zhihu) do(req *http.Request) ([]byte, error) {
	if z.secret == "" {
		return nil, fmt.Errorf("%w: 未配置 Access Secret", ErrUnauthorized)
	}
	req.Header.Set("Authorization", "Bearer "+z.secret)
	req.Header.Set("X-Request-Timestamp", strconv.FormatInt(z.now().Unix(), 10))
	req.Header.Set("Content-Type", "application/json")

	resp, err := z.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, fmt.Errorf("%w: 读取响应失败", ErrUnavailable)
	}
	switch resp.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return nil, fmt.Errorf("%w: %s", ErrUnauthorized, snippet(raw))
	case http.StatusTooManyRequests:
		return nil, fmt.Errorf("%w: %s", ErrRateLimited, snippet(raw))
	}
	if resp.StatusCode >= 500 {
		return nil, fmt.Errorf("%w: HTTP %d", ErrUnavailable, resp.StatusCode)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("%w: HTTP %d %s", ErrUnavailable, resp.StatusCode, snippet(raw))
	}
	return raw, nil
}

// codeError 把业务码翻成哨兵错误。30001/30002 是官方文档里的频限与配额。
func codeError(code int, msg string) error {
	switch code {
	case 20001:
		// 内容接口鉴权失败时仍返回 HTTP 200，只能靠业务码判断。
		return fmt.Errorf("%w: %s", ErrUnauthorized, msg)
	case 30001:
		return fmt.Errorf("%w: %s", ErrRateLimited, msg)
	case 30002:
		return fmt.Errorf("%w: %s", ErrQuotaExhausted, msg)
	default:
		return fmt.Errorf("%w: code=%d %s", ErrUnavailable, code, msg)
	}
}

func snippet(raw []byte) string {
	s := strings.TrimSpace(string(raw))
	if len(s) > 200 {
		return s[:200]
	}
	return s
}

func clampCount(n, max int) int {
	if n <= 0 {
		return 10
	}
	if n > max {
		return max
	}
	return n
}
