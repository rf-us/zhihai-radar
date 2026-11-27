package httpapi

import (
	"fmt"
	"net/http"
	"sync"
	"time"
)

// rateLimiter 保护知乎稀缺配额（热榜/直答各 100 次/天）。
//
// 两级限制：单会话 1 次/分钟（防连点）、全局 30 次/小时（防被扫）。
// 只挂在分析类端点上；登录、探针、只读页面不限流。
type rateLimiter struct {
	mu sync.Mutex

	perSession windowMap
	global     []time.Time

	sessionWindow time.Duration
	globalWindow  time.Duration
	globalMax     int
	now           func() time.Time
}

type windowMap map[string]time.Time

func newRateLimiter(now func() time.Time) *rateLimiter {
	if now == nil {
		now = time.Now
	}
	return &rateLimiter{
		perSession:    make(windowMap),
		sessionWindow: time.Minute,
		globalWindow:  time.Hour,
		globalMax:     30,
		now:           now,
	}
}

// allow 检查是否放行；返回 (ok, retryAfter)。
// 调用方拿到 ok=false 时回 429 并设置 Retry-After。
func (rl *rateLimiter) allow(sessionID string) (bool, time.Duration) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	now := rl.now()

	// 全局窗口：丢弃过期记录。
	cutoff := now.Add(-rl.globalWindow)
	i := 0
	for i < len(rl.global) && rl.global[i].Before(cutoff) {
		i++
	}
	if i > 0 {
		rl.global = rl.global[i:]
	}
	if len(rl.global) >= rl.globalMax {
		wait := rl.global[0].Add(rl.globalWindow).Sub(now)
		return false, wait
	}

	// 单会话窗口。
	if last, ok := rl.perSession[sessionID]; ok {
		if wait := last.Add(rl.sessionWindow).Sub(now); wait > 0 {
			return false, wait
		}
	}
	// 两个检查都通过才记账（429 不消耗配额）。
	rl.perSession[sessionID] = now
	rl.global = append(rl.global, now)
	return true, 0
}

// withAnalyzeRateLimit 只包分析端点；sessionID 取 radar_session cookie。
func (s *Server) withAnalyzeRateLimit(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.allowAnalysis(w, r) {
			next(w, r)
		}
	}
}

func (s *Server) allowAnalysis(w http.ResponseWriter, r *http.Request) bool {
	sid := ""
	if c, err := r.Cookie(cookieName); err == nil {
		sid = c.Value
	}
	ok, wait := s.limiter.allow(sid)
	if ok {
		return true
	}
	if wait < time.Second {
		wait = time.Second
	}
	w.Header().Set("Retry-After", fmt.Sprintf("%d", int(wait.Seconds())))
	writeErr(w, http.StatusTooManyRequests,
		fmt.Sprintf("分析过于频繁，请 %.0f 秒后再试（为保护知乎接口配额）", wait.Seconds()))
	return false
}
