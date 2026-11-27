package source

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func newZhihu(t *testing.T, h http.HandlerFunc) *Zhihu {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	z := NewZhihu(srv.URL, "secret-abc")
	z.now = func() time.Time { return time.Unix(1742822400, 0) }
	return z
}

func TestZhihuSearchMapsFields(t *testing.T) {
	var gotPath, gotQuery, gotAuth, gotTS string
	z := newZhihu(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery = r.URL.Path, r.URL.Query().Get("Query")
		gotAuth, gotTS = r.Header.Get("Authorization"), r.Header.Get("X-Request-Timestamp")
		fmt.Fprint(w, `{"Code":0,"Message":"success","Data":{"HasMore":false,"Items":[
			{"Title":"便携咖啡机值得买吗","ContentText":"清洗麻烦","Url":"https://www.zhihu.com/q/1",
			 "CommentCount":89,"VoteUpCount":312,"AuthorityLevel":"3","EditTime":1757606400}]}}`)
	})

	posts, err := z.SearchZhihu(context.Background(), "便携咖啡机", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if gotPath != "/api/v1/content/zhihu_search" {
		t.Fatalf("路径错误: %s", gotPath)
	}
	if gotQuery != "便携咖啡机" {
		t.Fatalf("查询词未编码正确: %q", gotQuery)
	}
	if gotAuth != "Bearer secret-abc" {
		t.Fatalf("鉴权头错误: %q", gotAuth)
	}
	if gotTS != "1742822400" {
		t.Fatalf("时间戳应为秒级: %q", gotTS)
	}
	if len(posts) != 1 {
		t.Fatalf("期望 1 条，得到 %d", len(posts))
	}
	p := posts[0]
	if p.Votes != 312 || p.Comments != 89 || p.Authority != 3 || p.URL == "" {
		t.Fatalf("字段映射错误: %+v", p)
	}
	if p.PublishedAt == nil || p.PublishedAt.Year() != 2025 {
		t.Fatalf("EditTime 应转成时间: %v", p.PublishedAt)
	}
}

func TestZhihuHotAndAsk(t *testing.T) {
	z := newZhihu(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/content/hot_list":
			fmt.Fprint(w, `{"Code":0,"Message":"success","Data":{"Total":1,"Items":[
				{"Title":"便携咖啡机为什么火了","Url":"https://www.zhihu.com/hot/1","ThumbnailUrl":"","Summary":"露营热"}]}}`)
		case "/v1/chat/completions":
			if r.Method != http.MethodPost {
				t.Errorf("直答必须是 POST")
			}
			var body struct {
				Model    string `json:"model"`
				Stream   bool   `json:"stream"`
				Messages []struct {
					Role    string `json:"role"`
					Content string `json:"content"`
				} `json:"messages"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body.Model == "" || len(body.Messages) == 0 || body.Stream {
				t.Errorf("请求体不符: %+v", body)
			}
			fmt.Fprint(w, `{"choices":[{"message":{"content":"{\"pain_points\":[]}"}}]}`)
		default:
			http.NotFound(w, r)
		}
	})

	hot, err := z.Hot(context.Background(), 30)
	if err != nil {
		t.Fatalf("hot: %v", err)
	}
	if len(hot) != 1 || hot[0].Source != "hot" || hot[0].Snippet != "露营热" {
		t.Fatalf("热榜映射错误: %+v", hot)
	}

	answer, err := z.Ask(context.Background(), "总结痛点")
	if err != nil {
		t.Fatalf("ask: %v", err)
	}
	if answer == "" {
		t.Fatal("直答应返回内容")
	}
}

func TestZhihuErrorMapping(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		body    string
		wantErr error
	}{
		{"401", http.StatusUnauthorized, `{"Message":"bad secret"}`, ErrUnauthorized},
		{"429", http.StatusTooManyRequests, `{"Message":"slow down"}`, ErrRateLimited},
		{"配额码", http.StatusOK, `{"Code":30002,"Message":"额度已用完"}`, ErrQuotaExhausted},
		{"频限码", http.StatusOK, `{"Code":30001,"Message":"请求过快"}`, ErrRateLimited},
		{"500", http.StatusInternalServerError, ``, ErrUnavailable},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			z := newZhihu(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(c.status)
				if c.body != "" {
					fmt.Fprint(w, c.body)
				}
			})
			_, err := z.SearchZhihu(context.Background(), "x", 10)
			if !errors.Is(err, c.wantErr) {
				t.Fatalf("期望 %v，得到 %v", c.wantErr, err)
			}
		})
	}
}

func TestParseAuthority(t *testing.T) {
	cases := map[string]float64{"3": 3, "4": 4, "9": 4, "-1": 0, "": 0, "high": 3, "优秀": 3, "unknown": 1}
	for in, want := range cases {
		if got := parseAuthority(in); got != want {
			t.Errorf("%q: 期望 %v，得到 %v", in, want, got)
		}
	}
}
