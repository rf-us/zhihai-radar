package config

import (
	"errors"
	"os"
	"strings"
)

type Config struct {
	Addr        string
	DataDir     string
	ReefBase    string
	ZhihuSecret string
	Source      string // zhihu | fixture
}

func Load() Config {
	return Config{
		Addr:        env("RADAR_ADDR", ":8090"),
		DataDir:     env("RADAR_DATA_DIR", "./data"),
		ReefBase:    env("REEF_BASE", "http://127.0.0.1:8080"),
		ZhihuSecret: os.Getenv("ZHIHU_ACCESS_SECRET"),
		Source:      env("RADAR_SOURCE", "zhihu"),
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
	return nil
}

func env(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}
