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

// TestReefStatusIncludesManagedState 验证托管状态被合进 /api/reef/status，
// 且只露出指纹前 8 位——完整指纹不得出现在 HTTP 响应里。
func TestReefStatusIncludesManagedState(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "radar.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}

	fullID := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	cfg := config.Config{ReefBase: "http://127.0.0.1:1", ReefDeviceID: fullID, Source: "fixture"}
	s := NewWithOptions(cfg, st, WithReefStatus(func() map[string]any {
		return map[string]any{
			"managed": true, "state": "down", "port": 8081,
			"deviceId": fullID[:8], "exitCode": 2,
		}
	}))
	s.now = time.Now

	// 直接塞一个会话，绕过登录（登录要连真实 reef）。
	const sid = "test-session-id"
	s.sessions[sid] = &session{
		token: "t", user: reef.User{Role: reef.RoleAdmin}, expires: time.Now().Add(time.Hour),
	}

	req := httptest.NewRequest(http.MethodGet, "/api/reef/status", nil)
	req.AddCookie(&http.Cookie{Name: cookieName, Value: sid})
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, body)
	}
	for _, want := range []string{`"managed":true`, `"state":"down"`, `"port":8081`, `"exitCode":2`, fullID[:8]} {
		if !strings.Contains(body, want) {
			t.Fatalf("响应缺少 %q：%s", want, body)
		}
	}
	if strings.Contains(body, fullID) {
		t.Fatalf("完整指纹泄漏到响应：%s", body)
	}
}
