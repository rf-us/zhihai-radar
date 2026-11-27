package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/rf-us/zhihai-radar/internal/config"
	"github.com/rf-us/zhihai-radar/internal/httpapi"
	"github.com/rf-us/zhihai-radar/internal/reefhost"
	"github.com/rf-us/zhihai-radar/internal/store"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// App 是桌面壳的绑定对象：前端只通过它拿到服务状态，不直接碰 RFERP。
type App struct {
	ctx context.Context

	mu      sync.Mutex
	server  *http.Server
	managed *reefhost.Managed
	st      *store.Store
	baseURL string
	version string
	ready   bool
	errMsg  string
}

func NewApp(version string) *App { return &App{version: version} }

// startupResult 是前端启动页拿到的状态。
type startupResult struct {
	Ready   bool   `json:"ready"`
	BaseURL string `json:"base_url"`
	Version string `json:"version"`
	Port    int    `json:"port"`
	Message string `json:"message,omitempty"`
}

// Startup 在窗口创建后调用：拉起运行环境 → 起内嵌 radar 服务 → 返回就绪状态。
// 任何失败都返回可操作信息，不让前端卡在白屏。
func (a *App) Startup() startupResult {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.ready {
		return a.snapshot()
	}

	cfg := config.Load()
	// 桌面端固定用本地数据目录，与服务端部署区分。
	dir := desktopDataDir()
	cfg.DataDir = dir
	// 桌面端默认 fixture（断网可用）；用户可设 RADAR_SOURCE=zhihu。
	if os.Getenv("RADAR_SOURCE") == "" {
		cfg.Source = "fixture"
	}
	// 桌面端托管运行环境（#2）；没有配置二进制时不强制，允许纯离线演示。
	cfg.ReefManaged = cfg.ReefManaged || os.Getenv("REEF_BIN") != ""

	if cfg.ReefManaged {
		m, err := reefhost.Start(reefhost.Options{Bin: cfg.ReefBin, Port: cfg.ReefPort})
		if err != nil {
			a.errMsg = "运行环境启动失败：" + err.Error()
			log.Println(a.errMsg)
			return a.snapshot()
		}
		a.managed = m
		if cfg.ReefDeviceID == "" {
			cfg.ReefDeviceID = m.DeviceID()
		}
		cfg.ReefBase = m.Base()
	}

	st, err := store.Open(filepath.Join(dir, "radar.db"))
	if err != nil {
		a.errMsg = "打开数据库失败：" + err.Error()
		return a.snapshot()
	}
	a.st = st
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := st.Migrate(ctx); err != nil {
		a.errMsg = "数据库迁移失败：" + err.Error()
		return a.snapshot()
	}

	// 随机端口：只绑 127.0.0.1，避免与并排打开的服务端冲突。
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		a.errMsg = "端口分配失败：" + err.Error()
		return a.snapshot()
	}
	port := ln.Addr().(*net.TCPAddr).Port
	a.baseURL = "http://127.0.0.1:" + strconv.Itoa(port)
	srv := &http.Server{
		Handler:           httpapi.New(cfg, st).Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	a.server = srv
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("内嵌服务异常：%v", err)
		}
	}()

	a.ready = true
	return a.snapshot()
}

func (a *App) snapshot() startupResult {
	out := startupResult{Ready: a.ready, BaseURL: a.baseURL, Version: a.version, Message: a.errMsg}
	if a.managed != nil {
		out.Port = a.managed.Port()
	}
	return out
}

// Status 供前端轮询启动状态。
func (a *App) Status() startupResult {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.snapshot()
}

// BaseURL 返回内嵌服务地址；前端所有请求走它 + 相对路径。
func (a *App) BaseURL() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.baseURL
}

// Quit 由前端退出时调用：关服务、关库、终止运行环境子进程。
func (a *App) Quit() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = a.server.Shutdown(ctx)
		cancel()
	}
	if a.st != nil {
		_ = a.st.Close()
	}
	if a.managed != nil {
		if err := a.managed.Close(); err != nil {
			log.Printf("关闭运行环境失败：%v", err)
		}
	}
	// 只有 Wails 注入过生命周期 context 才能调 runtime.Quit；
	// 单测里没有 GUI context，此时只清理资源，不退出进程。
	if a.ctx != nil {
		wailsruntime.Quit(a.ctx)
	}
}

// desktopDataDir 是 %LocalAppData%\zhihai-radar，覆盖安装不丢数据。
func desktopDataDir() string {
	if v := os.Getenv("RADAR_DATA_DIR"); v != "" {
		return v
	}
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		base = filepath.Join(os.Getenv("USERPROFILE"), "AppData", "Local")
	}
	return filepath.Join(base, "zhihai-radar")
}

// SetContext 由 main 在 Wails 启动时注入。
func (a *App) SetContext(ctx context.Context) { a.ctx = ctx }

// Version 供标题栏显示。
func (a *App) Version() string { return fmt.Sprintf("v%s", a.version) }

// Context 返回 Wails 注入的上下文，供单实例回调聚焦窗口使用。
func (a *App) Context() context.Context {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.ctx
}
