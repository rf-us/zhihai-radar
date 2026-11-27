// Package source 是外部数据源的适配层。
//
// 定义四个取数动作（知乎搜索 / 全网搜索 / 热榜 / 直答），由具体实现去对接；
// 编排、评分与落库都在 service 层，这里只管"拿到原始条目"。
//
// 目前有两个实现：
//   - Fixture：从本地 JSON 回放，用于离线演示与测试；
//   - 真实 HTTP 客户端：等拿到 ZHIHU_ACCESS_SECRET 与官方路径后补齐。
package source

import (
	"context"
	"errors"
	"fmt"
	"time"
)

type Post struct {
	Source      string     `json:"source"` // zhihu_search | global_search | hot
	Title       string     `json:"title"`
	Snippet     string     `json:"snippet"`
	URL         string     `json:"url"`
	Votes       int        `json:"votes"`
	Comments    int        `json:"comments"`
	Authority   float64    `json:"authority"` // 0–4
	PublishedAt *time.Time `json:"published_at,omitempty"`
}

type Source interface {
	SearchZhihu(ctx context.Context, keyword string, count int) ([]Post, error)
	SearchGlobal(ctx context.Context, keyword string, count int) ([]Post, error)
	Hot(ctx context.Context, limit int) ([]Post, error)
	// Ask 返回模型输出的文本；结构化解析由 service 负责。
	Ask(ctx context.Context, prompt string) (string, error)
}

// 上游错误分类。不做自动重试：配额类错误重试只会把当天的额度烧得更快。
var (
	ErrQuotaExhausted = errors.New("source: 配额已耗尽")
	ErrRateLimited    = errors.New("source: 触发频率限制")
	ErrUnauthorized   = errors.New("source: 凭证无效")
	ErrUnavailable    = errors.New("source: 上游不可用")
)

// Quota 是当日用量的快照，用于界面提示与降级判断。
type Quota struct {
	Used  int `json:"used"`
	Limit int `json:"limit"`
}

// Unavailable 是尚未配置真实数据源时的占位实现。
//
// 它不返回空数据、也不 panic，而是明确报"数据源未配置"——空数据会被当成
// "这个品类没热度"，是最糟糕的失败方式。
type Unavailable struct{ Reason string }

func (u Unavailable) err() error { return fmt.Errorf("%w: %s", ErrUnavailable, u.Reason) }

func (u Unavailable) SearchZhihu(context.Context, string, int) ([]Post, error) { return nil, u.err() }
func (u Unavailable) SearchGlobal(context.Context, string, int) ([]Post, error) {
	return nil, u.err()
}
func (u Unavailable) Hot(context.Context, int) ([]Post, error)    { return nil, u.err() }
func (u Unavailable) Ask(context.Context, string) (string, error) { return "", u.err() }
