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

	srv := &http.Server{
		Addr:              *addr,
		Handler:           httpapi.New(cfg, st).Handler(),
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
