package httpapi

import (
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// appRoutes 是前端 history 路由的入口。只有这些路径会回退到 index.html;
// 其余路径(尤其是 /assets/* 与 /api/*)保留各自的 404 语义,避免把
// "资源不存在"变成一张看起来正常的页面。
func isAppRoute(p string) bool {
	switch {
	case p == "/" || p == "/login":
		return true
	case p == "/opportunity" || strings.HasPrefix(p, "/opportunity/"):
		return true
	default:
		return false
	}
}

// newStaticHandler 提供前端静态资源,并处理 history 路由的直达与刷新。
//
// dist      前端产物视图(未构建时只有 .gitkeep)
// indexHTML dist/index.html 的内容;为 nil 表示前端未构建
// fallback  前端未构建时展示的占位页
func newStaticHandler(dist fs.FS, indexHTML, fallback []byte) http.Handler {
	built := len(indexHTML) > 0
	assets := http.FileServer(http.FS(dist))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		p := path.Clean(r.URL.Path)
		switch {
		case strings.HasPrefix(p, "/api/"):
			// 未被路由注册的 /api 路径:统一 JSON 404,不落到前端 shell。
			writeErr(w, http.StatusNotFound, "not found")
		case isAppRoute(p):
			if built {
				serveHTML(w, r, indexHTML)
			} else {
				serveHTML(w, r, fallback)
			}
		case !built:
			http.NotFound(w, r)
		case fileExists(dist, p):
			assets.ServeHTTP(w, r)
		default:
			http.NotFound(w, r)
		}
	})
}

func serveHTML(w http.ResponseWriter, r *http.Request, body []byte) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}
	_, _ = w.Write(body)
}

func fileExists(fsys fs.FS, urlPath string) bool {
	name := strings.TrimPrefix(path.Clean(urlPath), "/")
	if name == "" {
		return false
	}
	info, err := fs.Stat(fsys, name)
	return err == nil && !info.IsDir()
}
