package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/rf-us/zhihai-radar/internal/config"
	"github.com/rf-us/zhihai-radar/internal/httpapi"
	"github.com/rf-us/zhihai-radar/internal/reefhost"
	"github.com/rf-us/zhihai-radar/internal/store"
)

var version = "dev"

func main() {
	cfg := config.Load()
	addr := flag.String("addr", cfg.Addr, "HTTP listen address")
	flag.Parse()

	if err := cfg.Validate(); err != nil {
		log.Fatalf("配置有误：%v", err)
	}

	// 托管模式：拉起运行环境、抓主机指纹、探活，再用实际地址与指纹覆盖配置。
	// 必须在 httpapi.New 之前完成，否则 reef.Client 会拿着旧 base/空指纹构造。
	var managed *reefhost.Managed
	if cfg.ReefManaged {
		m, err := reefhost.Start(reefhost.Options{
			Bin:  cfg.ReefBin,
			Port: cfg.ReefPort,
		})
		if err != nil {
			log.Fatalf("托管运行环境失败：%v", err)
		}
		managed = m
		// 手工指定的 REEF_DEVICE_ID 优先级更高：覆盖自动抓到的指纹，
		// 供"指纹抓取失败但人工知道"的现场救急。
		if cfg.ReefDeviceID == "" {
			cfg.ReefDeviceID = m.DeviceID()
		}
		cfg.ReefBase = m.Base()
		defer func() {
			if err := managed.Close(); err != nil {
				log.Printf("关闭运行环境失败：%v", err)
			}
		}()
	}

	st, err := store.Open(filepath.Join(cfg.DataDir, "radar.db"))
	if err != nil {
		log.Fatalf("打开数据库失败：%v", err)
	}
	defer st.Close()

	migrateCtx, cancelMigrate := context.WithTimeout(context.Background(), 20*time.Second)
	if err := st.Migrate(migrateCtx); err != nil {
		cancelMigrate()
		log.Fatalf("数据库迁移失败：%v", err)
	}
	cancelMigrate()

	var handler http.Handler
	if managed != nil {
		handler = httpapi.NewWithOptions(cfg, st, httpapi.WithReefStatus(func() map[string]any {
			st := managed.GetStatus()
			return map[string]any{
				"managed":  st.Managed,
				"state":    st.State,
				"port":     st.Port,
				"deviceId": st.DeviceID, // 仅前 8 位
				"exitCode": st.ExitCode,
			}
		})).Handler()
	} else {
		handler = httpapi.New(cfg, st).Handler()
	}

	srv := &http.Server{
		Addr:              *addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Printf("zhihai-radar %s listening on %s (reef=%s, data=%s)",
			version, *addr, cfg.ReefBase, cfg.DataDir)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("HTTP 服务异常退出：%v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("关闭时出错：%v", err)
	}
	log.Println("bye")
}
