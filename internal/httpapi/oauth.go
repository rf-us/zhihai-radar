package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/rf-us/zhihai-radar/internal/reef"
)

const (
	stateCookieName     = "radar_oauth_state"
	defaultAuthorizeURL = "https://openapi.zhihu.com/authorize"
	defaultTokenURL     = "https://openapi.zhihu.com/access_token"
	defaultUserURL      = "https://openapi.zhihu.com/user"
)

// handleAuthConfig GET /api/auth/config（免登录）：前端据此显示/隐藏知乎登录入口。
func (s *Server) handleAuthConfig(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"enabled": s.cfg.OAuthEnabled()})
}

// handleZhihuAuthStart GET /api/auth/zhihu/start：302 到知乎授权页。
func (s *Server) handleZhihuAuthStart(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.OAuthEnabled() {
		writeErr(w, http.StatusServiceUnavailable, "未配置知乎 OAuth，入口不可用")
		return
	}
	state, err := newSessionID() // 复用 32 字节随机串
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "无法生成授权状态")
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: stateCookieName, Value: state, Path: "/api/auth/zhihu",
		HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: 600,
	})
	q := url.Values{}
	q.Set("app_id", s.cfg.ZhihuOAuthAppID)
	q.Set("redirect_uri", s.cfg.ZhihuOAuthRedirectURI)
	q.Set("response_type", "code")
	q.Set("state", state)
	http.Redirect(w, r, s.oauthAuthorizeURL+"?"+q.Encode(), http.StatusFound)
}

// zhihuUser 只取公开资料（昵称/头像），不申请额外权限。
type zhihuUser struct {
	UID      int64  `json:"uid"`
	HashID   string `json:"hash_id"`
	Fullname string `json:"fullname"`
	Avatar   string `json:"avatar_path"`
}

// handleZhihuAuthCallback GET /api/auth/zhihu/callback：code 换 token → 取用户信息 → 建本地会话。
func (s *Server) handleZhihuAuthCallback(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.OAuthEnabled() {
		writeErr(w, http.StatusServiceUnavailable, "未配置知乎 OAuth")
		return
	}
	// 资料说明回调可能不返 state；有则返回时必须一致，防 CSRF。
	if state := r.URL.Query().Get("state"); state != "" {
		c, err := r.Cookie(stateCookieName)
		if err != nil || c.Value != state {
			writeErr(w, http.StatusBadRequest, "授权状态不匹配，请重试")
			return
		}
	}
	code := r.URL.Query().Get("authorization_code")
	if code == "" {
		code = r.URL.Query().Get("code") // 兼容协议修订
	}
	if code == "" {
		writeErr(w, http.StatusBadRequest, "授权被拒绝或未返回授权码")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	token, err := s.exchangeZhihuCode(ctx, code)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "换取知乎授权失败："+err.Error())
		return
	}
	user, err := s.fetchZhihuUser(ctx, token)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "读取知乎用户信息失败："+err.Error())
		return
	}

	// 建立应用自己的会话；OAuth token 只存内存，不下发前端。
	id, err := newSessionID()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "会话不可用")
		return
	}
	s.mu.Lock()
	s.sessions[id] = &session{
		token: "", // OAuth 会话不需要 reef token
		user: reef.User{
			ID: user.UID, Username: user.Fullname, Role: "viewer",
		},
		expires: s.now().Add(sessionTTL),
	}
	s.mu.Unlock()
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: id, Path: "/", HttpOnly: true,
		SameSite: http.SameSiteLaxMode, MaxAge: int(sessionTTL.Seconds()),
	})
	// 清状态 cookie；回前端首页，不把 token 放进 URL。
	http.SetCookie(w, &http.Cookie{Name: stateCookieName, Value: "", Path: "/api/auth/zhihu", MaxAge: -1})
	http.Redirect(w, r, "/", http.StatusFound)
}

func (s *Server) exchangeZhihuCode(ctx context.Context, code string) (string, error) {
	form := url.Values{}
	form.Set("app_id", s.cfg.ZhihuOAuthAppID)
	form.Set("app_key", s.cfg.ZhihuOAuthAppKey)
	form.Set("grant_type", "authorization_code")
	form.Set("redirect_uri", s.cfg.ZhihuOAuthRedirectURI)
	form.Set("code", code)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.oauthTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var out struct {
		AccessToken string `json:"access_token"`
		Message     string `json:"message"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK || out.AccessToken == "" {
		return "", fmt.Errorf("状态码 %d：%s", resp.StatusCode, out.Message)
	}
	return out.AccessToken, nil
}

func (s *Server) fetchZhihuUser(ctx context.Context, token string) (zhihuUser, error) {
	var out zhihuUser
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.oauthUserURL, nil)
	if err != nil {
		return out, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return out, err
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return out, err
	}
	// 只认有效用户标识（uid 或 hash_id 至少一个）。
	if out.UID == 0 && strings.TrimSpace(out.HashID) == "" {
		return out, errors.New("响应中没有有效用户标识")
	}
	return out, nil
}
