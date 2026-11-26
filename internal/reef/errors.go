package reef

import (
	"errors"
	"fmt"
	"net/http"
)

// ErrUnreachable 表示请求没有拿到任何 HTTP 响应：连接失败、DNS 失败、超时。
//
// 这类错误对写操作有特殊含义——**结果未知**。产品可能已经建成，也可能没有。
// 调用方必须先回查再决定是否重试，不能直接重试（否则会建出两条产品）。
var ErrUnreachable = errors.New("reef: unreachable")

// APIError 是 RFERP 返回的非 2xx 响应。
//
// Message 保留 RFERP 的中文原文，直接展示给用户即可：它的校验收敛在服务层，
// 比 radar 自己再编一套提示更准确。
type APIError struct {
	Op      string
	Status  int
	Message string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("reef: %s: %d %s", e.Op, e.Status, e.Message)
}

func (e *APIError) IsUnauthorized() bool { return e.Status == http.StatusUnauthorized }
func (e *APIError) IsForbidden() bool    { return e.Status == http.StatusForbidden }
func (e *APIError) IsNotFound() bool     { return e.Status == http.StatusNotFound }

// IsServerFault 表示 5xx：服务端出错，写操作的结果同样不可信。
func (e *APIError) IsServerFault() bool { return e.Status >= 500 }

// ErrProductExists 表示目标编码已存在，且看起来就是本候选之前采纳出来的产品。
type ErrProductExists struct {
	ProductID int64
	Code      string
}

func (e *ErrProductExists) Error() string {
	return fmt.Sprintf("reef: 产品已存在（code=%s, id=%d）", e.Code, e.ProductID)
}

// ErrCodeTaken 表示编码被别的产品占用（不是本候选的采纳记录）。
// 这与 ErrProductExists 必须分开：前者可以直接复用，后者需要用户改编码。
var ErrCodeTaken = errors.New("reef: 产品编码已被其他产品占用")

// IsUnknownOutcome 判断一次写操作失败后结果是否不可信。
func IsUnknownOutcome(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrUnreachable) {
		return true
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.IsServerFault()
	}
	return false
}
