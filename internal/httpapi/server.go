// Package httpapi 是 radar 的 HTTP 面。
//
// 浏览器只与本服务通信；所有对 RFERP 的调用都在这里（服务端）完成，
// reef 的 token 也只存在本进程内存里，不下发前端。
package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/rf-us/zhihai-radar/internal/config"
	"github.com/rf-us/zhihai-radar/internal/reef"
	"github.com/rf-us/zhihai-radar/internal/store"
	webassets "github.com/rf-us/zhihai-radar/web"
)

const (
	cookieName   = "radar_session"
	sessionTTL   = 8 * time.Hour
	loginTimeout = 20 * time.Second
	probeTimeout = 6 * time.Second
)

type session struct {
	token   string
	user    reef.User
	expires time.Time
}

type Server struct {
	cfg  config.Config
	st   *store.Store
	reef *reef.Client

	mu       sync.Mutex
	sessions map[string]*session

	now func() time.Time
}

func New(cfg config.Config, st *store.Store) *Server {
	return &Server{
		cfg:      cfg,
		st:       st,
		reef:     reef.New(cfg.ReefBase).WithDevice(cfg.ReefDeviceID),
		sessions: make(map[string]*session),
		now:      time.Now,
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("POST /api/session", s.handleLogin)
	mux.HandleFunc("GET /api/session", s.handleSession)
	mux.HandleFunc("DELETE /api/session", s.handleLogout)
	mux.HandleFunc("GET /api/reef/status", s.requireSession(s.handleReefStatus))
	mux.HandleFunc("GET /api/reef/parts", s.requireSession(s.handleParts))
	mux.Handle("/", http.FileServer(http.FS(webassets.FS())))
	return s.withLogging(mux)
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":   "ok",
		"time":     s.now().UTC().Format(time.RFC3339),
		"reefBase": s.cfg.ReefBase,
	})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	body.Username = strings.TrimSpace(body.Username)
	if body.Username == "" || body.Password == "" {
		writeErr(w, http.StatusBadRequest, "用户名和密码不能为空")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), loginTimeout)
	defer cancel()

	res, err := s.reef.Login(ctx, body.Username, body.Password)
	if err != nil {
		writeReefErr(w, err)
		return
	}

	id, err := newSessionID()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "session unavailable")
		return
	}
	s.mu.Lock()
	s.sessions[id] = &session{token: res.Token, user: res.User, expires: s.now().Add(sessionTTL)}
	s.mu.Unlock()

	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    id,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(sessionTTL.Seconds()),
	})
	writeJSON(w, http.StatusOK, map[string]any{"user": res.User, "reefBase": s.cfg.ReefBase})
}

func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.session(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": sess.user, "reefBase": s.cfg.ReefBase})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(cookieName); err == nil {
		s.mu.Lock()
		delete(s.sessions, c.Value)
		s.mu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", HttpOnly: true, MaxAge: -1})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleReefStatus(w http.ResponseWriter, r *http.Request, sess *session) {
	ctx, cancel := context.WithTimeout(r.Context(), probeTimeout)
	defer cancel()

	out := map[string]any{
		"connected": false,
		"user":      sess.user,
		"canAdopt":  sess.user.Role == reef.RoleAdmin,
		"reefBase":  s.cfg.ReefBase,
	}
	info, err := s.reef.Probe(ctx)
	if err != nil {
		out["message"] = "无法连接 RFERP：" + err.Error()
		writeJSON(w, http.StatusOK, out)
		return
	}
	out["connected"] = true
	out["reef"] = info
	out["compatible"] = info.APIVersion == reef.SupportedAPIVersion
	if info.APIVersion != reef.SupportedAPIVersion {
		out["message"] = fmt.Sprintf("RFERP 接口版本为 %d，本工具只验证过 %d，可能不兼容",
			info.APIVersion, reef.SupportedAPIVersion)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleParts(w http.ResponseWriter, r *http.Request, sess *session) {
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	parts, err := s.reef.ListParts(ctx, sess.token)
	if err != nil {
		writeReefErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, parts)
}

func (s *Server) requireSession(next func(http.ResponseWriter, *http.Request, *session)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sess, ok := s.session(r)
		if !ok {
			writeErr(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		next(w, r, sess)
	}
}

func (s *Server) session(r *http.Request) (*session, bool) {
	c, err := r.Cookie(cookieName)
	if err != nil {
		return nil, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[c.Value]
	if !ok {
		return nil, false
	}
	if s.now().After(sess.expires) {
		delete(s.sessions, c.Value)
		return nil, false
	}
	return sess, true
}

func (s *Server) withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := s.now()
		next.ServeHTTP(w, r)
		// 不记录请求体与 token；只留方法与路径，便于排查。
		log.Printf("%s %s %s", r.Method, r.URL.Path, s.now().Sub(start).Round(time.Millisecond))
	})
}

func newSessionID() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

// writeReefErr 把 RFERP 的失败翻译成 HTTP。401/403 原样透出：
// 前端据此提示"会话失效"或"需要管理员账号"，比笼统的 500 有用。
func writeReefErr(w http.ResponseWriter, err error) {
	var apiErr *reef.APIError
	switch {
	case errors.As(err, &apiErr):
		writeErr(w, apiErr.Status, apiErr.Message)
	case errors.Is(err, reef.ErrUnreachable):
		writeErr(w, http.StatusBadGateway, "无法连接 RFERP："+err.Error())
	default:
		writeErr(w, http.StatusInternalServerError, err.Error())
	}
}
