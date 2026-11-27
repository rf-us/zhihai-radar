package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rf-us/zhihai-radar/internal/config"
	"github.com/rf-us/zhihai-radar/internal/reef"
	"github.com/rf-us/zhihai-radar/internal/store"
)

func TestAnalyzeRateLimitSecondClick(t *testing.T) {
	st := newSupplyTestStoreForLimit(t)
	s := NewWithOptions(configForLimit(), st)
	// 注入固定时钟，避免测试跨分钟不稳定。
	now := time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)
	s.limiter = newRateLimiter(func() time.Time { return now })

	const sid = "limit-session"
	s.sessions[sid] = &session{token: "t", user: reefUserForLimit(), expires: now.Add(time.Hour)}

	do := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/analyze", stringReader(`{"keyword":"咖啡机"}`))
		req.AddCookie(&http.Cookie{Name: cookieName, Value: sid})
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		return rec
	}
	// 第一次会被限流放行，随后进入 analyze（fixture 数据源可能失败，但状态码不是 429）。
	first := do()
	if first.Code == http.StatusTooManyRequests {
		t.Fatalf("第一次不应被限流：%d %s", first.Code, first.Body.String())
	}
	second := do()
	if second.Code != http.StatusTooManyRequests {
		t.Fatalf("第二次应 429，got %d %s", second.Code, second.Body.String())
	}
	if second.Header().Get("Retry-After") == "" {
		t.Fatal("429 应带 Retry-After")
	}
}

func TestRateLimiterDoesNotChargeOnReject(t *testing.T) {
	now := time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)
	rl := newRateLimiter(func() time.Time { return now })
	if ok, _ := rl.allow("s1"); !ok {
		t.Fatal("首次应放行")
	}
	if ok, _ := rl.allow("s1"); ok {
		t.Fatal("同会话第二次应拒绝")
	}
	if ok, _ := rl.allow("s2"); !ok {
		t.Fatal("被拒的请求不应消耗全局配额，另一个会话应放行")
	}
}

func stringReader(s string) *strings.Reader { return strings.NewReader(s) }
func configForLimit() config.Config         { return config.Config{Source: "fixture"} }
func reefUserForLimit() reef.User           { return reef.User{Role: reef.RoleAdmin} }
func newSupplyTestStoreForLimit(t *testing.T) *store.Store {
	st, err := store.Open(filepath.Join(t.TempDir(), "radar.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	return st
}
