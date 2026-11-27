package httpapi

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

var testIndex = []byte("<!doctype html><title>app</title><div id=root></div>")
var testFallback = []byte("<!doctype html><title>前端未构建</title>")

func builtFS() fs.FS {
	return fstest.MapFS{
		"index.html":    {Data: testIndex},
		"assets/app.js": {Data: []byte("console.log(1)")},
	}
}

func emptyFS() fs.FS {
	return fstest.MapFS{".gitkeep": {Data: []byte{}}}
}

func do(t *testing.T, h http.Handler, method, target string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// history 路由(含深链接)必须返回外壳,刷新不 404。
func TestAppRoutesReturnShell(t *testing.T) {
	h := newStaticHandler(builtFS(), testIndex, testFallback)
	for _, p := range []string{"/", "/login", "/opportunity/42", "/opportunity/42/report"} {
		rec := do(t, h, http.MethodGet, p)
		if rec.Code != http.StatusOK {
			t.Errorf("%s: status = %d, want 200", p, rec.Code)
		}
		if got := rec.Body.String(); got != string(testIndex) {
			t.Errorf("%s: body = %q, want index shell", p, got)
		}
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
			t.Errorf("%s: content-type = %q", p, ct)
		}
	}
}

// 静态资源保持各自语义:存在即返回,缺失就 404,不落回前端外壳。
func TestStaticAssetSemantics(t *testing.T) {
	h := newStaticHandler(builtFS(), testIndex, testFallback)

	rec := do(t, h, http.MethodGet, "/assets/app.js")
	if rec.Code != http.StatusOK {
		t.Errorf("existing asset: status = %d, want 200", rec.Code)
	}
	if got := rec.Body.String(); got != "console.log(1)" {
		t.Errorf("existing asset body = %q", got)
	}

	rec = do(t, h, http.MethodGet, "/assets/missing.js")
	if rec.Code != http.StatusNotFound {
		t.Errorf("missing asset: status = %d, want 404", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "app") {
		t.Error("missing asset must not fall back to the SPA shell")
	}
}

// 未注册的 /api 路径返回 JSON 404,不返回 HTML。
func TestUnknownAPIIsJSON404(t *testing.T) {
	h := newStaticHandler(builtFS(), testIndex, testFallback)
	rec := do(t, h, http.MethodGet, "/api/nope")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("content-type = %q, want JSON", ct)
	}
	if got := rec.Body.String(); !strings.Contains(got, `"error"`) {
		t.Errorf("body = %q, want {\"error\":...}", got)
	}
}

// 没有嵌入产物时,仍然给出可读的占位页,而不是 404 或空白。
func TestUnbuiltServesFallback(t *testing.T) {
	h := newStaticHandler(emptyFS(), nil, testFallback)
	for _, p := range []string{"/", "/opportunity/42/report"} {
		rec := do(t, h, http.MethodGet, p)
		if rec.Code != http.StatusOK {
			t.Errorf("%s: status = %d, want 200", p, rec.Code)
		}
		if got := rec.Body.String(); got != string(testFallback) {
			t.Errorf("%s: body = %q, want fallback", p, got)
		}
	}
	// 未构建时静态资源同样 404。
	if rec := do(t, h, http.MethodGet, "/assets/app.js"); rec.Code != http.StatusNotFound {
		t.Errorf("asset while unbuilt: status = %d, want 404", rec.Code)
	}
}

func TestStaticRejectsNonGET(t *testing.T) {
	h := newStaticHandler(builtFS(), testIndex, testFallback)
	rec := do(t, h, http.MethodPost, "/")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
	if allow := rec.Header().Get("Allow"); allow != "GET, HEAD" {
		t.Errorf("Allow = %q", allow)
	}
}

func TestIsAppRoute(t *testing.T) {
	cases := map[string]bool{
		"/":                     true,
		"/login":                true,
		"/opportunity":          true,
		"/opportunity/7":        true,
		"/opportunity/7/report": true,
		"/opportunityx":         false,
		"/api/session":          false,
		"/assets/app.js":        false,
	}
	for p, want := range cases {
		if got := isAppRoute(p); got != want {
			t.Errorf("isAppRoute(%q) = %v, want %v", p, got, want)
		}
	}
}
