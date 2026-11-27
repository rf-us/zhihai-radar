package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rf-us/zhihai-radar/internal/config"
	"github.com/rf-us/zhihai-radar/internal/store"
)

func TestAuthConfigHiddenWhenNotConfigured(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "radar.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestStore(t, st)
	s := New(config.Config{Source: "fixture"}, st)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/auth/config", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "false") {
		t.Fatalf("未配置应隐藏入口：%d %s", rec.Code, rec.Body.String())
	}
}

func TestAuthConfigVisibleWhenConfigured(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "radar.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestStore(t, st)
	cfg := config.Config{Source: "fixture", ZhihuOAuthAppID: "id", ZhihuOAuthAppKey: "key", ZhihuOAuthRedirectURI: "http://localhost/cb"}
	s := New(cfg, st)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/auth/config", nil))
	if !strings.Contains(rec.Body.String(), "true") {
		t.Fatalf("配置齐全应显示入口：%s", rec.Body.String())
	}
}

func TestZhihuAuthStartRedirects(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "radar.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestStore(t, st)
	cfg := config.Config{Source: "fixture", ZhihuOAuthAppID: "id", ZhihuOAuthAppKey: "key", ZhihuOAuthRedirectURI: "http://localhost/cb"}
	s := New(cfg, st)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/auth/zhihu/start", nil))
	if rec.Code != http.StatusFound {
		t.Fatalf("应 302，got %d %s", rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("Location")
	if !strings.Contains(loc, "app_id=id") || !strings.Contains(loc, "state=") {
		t.Fatalf("授权 URL 不完整：%s", loc)
	}
}

func TestZhihuAuthCallbackWithFakeEndpoints(t *testing.T) {
	// 假知乎服务：换 token + 取用户。
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/access_token":
			fmt.Fprint(w, `{"access_token":"tok"}`)
		case "/user":
			if r.Header.Get("Authorization") != "Bearer tok" {
				t.Errorf("用户接口应带 Bearer token")
			}
			fmt.Fprint(w, `{"uid":123,"fullname":"评委用户"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer fake.Close()
	st, err := store.Open(filepath.Join(t.TempDir(), "radar.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestStore(t, st)
	_ = st.Migrate(context.Background())
	cfg := config.Config{Source: "fixture", ZhihuOAuthAppID: "id", ZhihuOAuthAppKey: "key", ZhihuOAuthRedirectURI: "http://localhost/cb"}
	s := NewWithOptions(cfg, st, WithOAuthEndpoints(fake.URL+"/authorize", fake.URL+"/access_token", fake.URL+"/user"))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/auth/zhihu/callback?authorization_code=abc&state=xyz", nil)
	req.AddCookie(&http.Cookie{Name: stateCookieName, Value: "xyz"})
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/" {
		t.Fatalf("回调应回首页：%d %s", rec.Code, rec.Header().Get("Location"))
	}
	// 会话应建立，且不含 token。
	var sess *session
	for _, v := range s.sessions {
		if v.user.Username == "评委用户" {
			sess = v
		}
	}
	if sess == nil || sess.token != "" {
		t.Fatalf("OAuth 会话建立失败或泄露 token：%+v", sess)
	}
	_ = time.Now()
}
