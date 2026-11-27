package reef

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Client 是 RFERP 的 HTTP 客户端。
//
// 刻意做成**无状态**：token 由调用方逐次传入。这样不会出现"客户端里藏着一个
// 已过期的 token"这种难查的问题，也让并发调用与测试都更简单。
type Client struct {
	base string
	hc   *http.Client

	// deviceID 对应 RFERP 2.x 局域网版的准入要求。
	//
	// 2.1.1 起，登录与**每一个**已认证请求都必须带 X-RFERP-Device-ID，且与
	// 会话绑定的设备一致；不携带会被当作 device_missing，换了值则 401。
	// 未导入授权时只有"主机"指纹能登录——radar 与服务端同机部署时，填主机指纹即可。
	deviceID string
}

func New(base string) *Client {
	return &Client{
		base: strings.TrimRight(strings.TrimSpace(base), "/"),
		hc:   &http.Client{Timeout: 20 * time.Second},
	}
}

// WithDevice 设置设备指纹；空值表示目标是不要求设备标识的旧版 RFERP（≤ 4d73ae7）。
func (c *Client) WithDevice(id string) *Client {
	c.deviceID = strings.TrimSpace(id)
	return c
}

func (c *Client) DeviceID() string { return c.deviceID }

// NewWithHTTPClient 供测试注入短超时。
func NewWithHTTPClient(base string, hc *http.Client) *Client {
	c := New(base)
	if hc != nil {
		c.hc = hc
	}
	return c
}

func (c *Client) Base() string { return c.base }

// Probe 读取 RFERP 的自描述。免登录，用于连接页的"测试连接"。
func (c *Client) Probe(ctx context.Context) (ServerInfo, error) {
	var out ServerInfo
	raw, err := c.do(ctx, "serverinfo", http.MethodGet, "/api/v1/serverinfo", "", nil)
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return out, fmt.Errorf("reef: decode serverinfo: %w", err)
	}
	return out, nil
}

func (c *Client) Login(ctx context.Context, username, password string) (LoginResult, error) {
	var out LoginResult
	body := map[string]string{"username": username, "password": password}
	raw, err := c.do(ctx, "login", http.MethodPost, "/api/login", "", body)
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return out, fmt.Errorf("reef: decode login: %w", err)
	}
	if out.Token == "" {
		return out, fmt.Errorf("reef: login returned empty token")
	}
	return out, nil
}

func (c *Client) Me(ctx context.Context, token string) (User, error) {
	var out User
	raw, err := c.do(ctx, "me", http.MethodGet, "/api/me", token, nil)
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return out, fmt.Errorf("reef: decode me: %w", err)
	}
	return out, nil
}

func (c *Client) ListParts(ctx context.Context, token string) ([]Part, error) {
	raw, err := c.do(ctx, "list parts", http.MethodGet, "/api/parts", token, nil)
	if err != nil {
		return nil, err
	}
	return decodeList[Part](raw)
}

func (c *Client) ListProducts(ctx context.Context, token string) ([]Product, error) {
	raw, err := c.do(ctx, "list products", http.MethodGet, "/api/products", token, nil)
	if err != nil {
		return nil, err
	}
	return decodeList[Product](raw)
}

func (c *Client) CreateProduct(ctx context.Context, token string, in CreateProductInput) (Product, error) {
	var out Product
	if in.Status == 0 {
		in.Status = 1
	}
	raw, err := c.do(ctx, "create product", http.MethodPost, "/api/products", token, in)
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return out, fmt.Errorf("reef: decode create product: %w", err)
	}
	return out, nil
}

func (c *Client) DeleteProduct(ctx context.Context, token string, id int64) error {
	path := "/api/products?id=" + strconv.FormatInt(id, 10)
	_, err := c.do(ctx, "delete product", http.MethodDelete, path, token, nil)
	return err
}

func (c *Client) AddBOMItem(ctx context.Context, token string, in BOMInput) (BOMItem, error) {
	var out BOMItem
	raw, err := c.do(ctx, "add bom item", http.MethodPost, "/api/bom", token, in)
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return out, fmt.Errorf("reef: decode bom item: %w", err)
	}
	return out, nil
}

func (c *Client) GetBOM(ctx context.Context, token string, productID int64) ([]BOMItem, error) {
	path := "/api/bom?product_id=" + strconv.FormatInt(productID, 10)
	raw, err := c.do(ctx, "get bom", http.MethodGet, path, token, nil)
	if err != nil {
		return nil, err
	}
	return decodeList[BOMItem](raw)
}

func (c *Client) do(ctx context.Context, op, method, path, token string, in any) ([]byte, error) {
	if c.base == "" {
		return nil, fmt.Errorf("reef: %s: base url is empty", op)
	}
	var body io.Reader
	if in != nil {
		raw, err := json.Marshal(in)
		if err != nil {
			return nil, fmt.Errorf("reef: encode %s: %w", op, err)
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, body)
	if err != nil {
		return nil, fmt.Errorf("reef: build %s: %w", op, err)
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if c.deviceID != "" {
		req.Header.Set("X-RFERP-Device-ID", c.deviceID)
		req.Header.Set("X-RFERP-Client-OS", runtime.GOOS)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		// 注意：这里包的是 ErrUnreachable，写操作上层据此判定"结果未知"。
		return nil, fmt.Errorf("%w: %s: %v", ErrUnreachable, op, err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, fmt.Errorf("%w: %s: read body: %v", ErrUnreachable, op, err)
	}
	if resp.StatusCode >= 400 {
		return nil, &APIError{Op: op, Status: resp.StatusCode, Message: errorMessage(raw, resp.Status)}
	}
	return raw, nil
}

// decodeList 容忍 RFERP 返回 null。
//
// Go 的 var list []T 序列化就是 null，RFERP 的列表端点在零行时确实会这么返回。
// 直接把 null 交给前端，页面会在 .map 上白屏。
func decodeList[T any](raw []byte) ([]T, error) {
	out := make([]T, 0)
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return out, nil
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("reef: decode list: %w", err)
	}
	return out, nil
}

func errorMessage(raw []byte, fallback string) string {
	var e struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(raw, &e) == nil && strings.TrimSpace(e.Error) != "" {
		return e.Error
	}
	if s := strings.TrimSpace(string(raw)); s != "" && len(s) <= 300 {
		return s
	}
	return fallback
}
