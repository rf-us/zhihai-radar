package config

import (
	"errors"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Addr        string
	DataDir     string
	ReefBase    string
	ZhihuSecret string
	Source      string // zhihu | fixture

	// ReefDeviceID 是 RFERP 2.x 局域网版要求的设备指纹。
	// 未导入授权时，只有"主机"指纹能登录；与服务端同机部署时填 hosts 表里的那条。
	ReefDeviceID string

	// 托管模式：REEF_MANAGED=1 时由 radar 拉起并监管运行环境子进程，
	// 自动解析主机指纹、按 REEF_PORT 顺延端口。外部模式（默认）保持原行为。
	ReefManaged bool
	ReefBin     string
	ReefPort    int

	// ReadOnly 为 true 时禁止采纳写回（公网 Demo 默认只读，防被扫写脏 RFERP）。
	ReadOnly bool

	// 知乎 OAuth（可选）：三者都配齐才启用，入口由 /api/auth/config 告知前端。
	ZhihuOAuthAppID       string
	ZhihuOAuthAppKey      string
	ZhihuOAuthRedirectURI string
}

// OAuthEnabled 表示钩子齐全，可以展示知乎登录入口。
func (c Config) OAuthEnabled() bool {
	return strings.TrimSpace(c.ZhihuOAuthAppID) != "" &&
		strings.TrimSpace(c.ZhihuOAuthAppKey) != "" &&
		strings.TrimSpace(c.ZhihuOAuthRedirectURI) != ""
}

func Load() Config {
	return Config{
		Addr:        env("RADAR_ADDR", ":8090"),
		DataDir:     env("RADAR_DATA_DIR", "./data"),
		ReefBase:    env("REEF_BASE", "http://127.0.0.1:8080"),
		ZhihuSecret: os.Getenv("ZHIHU_ACCESS_SECRET"),
		Source:      env("RADAR_SOURCE", "zhihu"),

		ReefDeviceID: os.Getenv("REEF_DEVICE_ID"),

		ReefManaged: strings.EqualFold(strings.TrimSpace(os.Getenv("REEF_MANAGED")), "1"),
		ReefBin:     strings.TrimSpace(os.Getenv("REEF_BIN")),
		ReefPort:    envInt("REEF_PORT", 8080),

		ReadOnly: strings.EqualFold(strings.TrimSpace(os.Getenv("RADAR_READONLY")), "1"),

		ZhihuOAuthAppID:       os.Getenv("ZHIHU_OAUTH_APP_ID"),
		ZhihuOAuthAppKey:      os.Getenv("ZHIHU_OAUTH_APP_KEY"),
		ZhihuOAuthRedirectURI: os.Getenv("ZHIHU_OAUTH_REDIRECT_URI"),
	}
}

func (c Config) Validate() error {
	if !strings.HasPrefix(c.ReefBase, "http://") && !strings.HasPrefix(c.ReefBase, "https://") {
		return errors.New("REEF_BASE 必须以 http:// 或 https:// 开头")
	}
	switch c.Source {
	case "zhihu", "fixture":
	default:
		return errors.New("RADAR_SOURCE 只能是 zhihu 或 fixture")
	}
	if c.Addr == "" {
		return errors.New("RADAR_ADDR 不能为空")
	}
	if c.ReefManaged && c.ReefBin == "" {
		return errors.New("托管模式需要 REEF_BIN 指定运行环境可执行文件")
	}
	return nil
}

func env(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 || n > 65535 {
		return fallback
	}
	return n
}

// FixtureDir 是离线快照目录（RADAR_SOURCE=fixture 时读取）。
func (c Config) FixtureDir() string { return env("RADAR_FIXTURE_DIR", "./fixtures") }

// ZhihuBase 允许指向本地假服务（测试/联调）；留空用官方域名。
func (c Config) ZhihuBase() string { return os.Getenv("ZHIHU_BASE") }
