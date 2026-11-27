package main

import (
	"context"
	"embed"
	"log"
	"os"
	"strings"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

var version = "dev"

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	app := NewApp(version)

	err := wails.Run(&options.App{
		Title:     "知海选品雷达 " + version,
		Width:     1280,
		Height:    800,
		MinWidth:  1024,
		MinHeight: 700,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 247, G: 247, B: 245, A: 1},
		OnStartup: func(ctx context.Context) {
			app.SetContext(ctx)
			app.Startup()
		},
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId: "zhihai-radar-desktop",
			OnSecondInstanceLaunch: func(_ options.SecondInstanceData) {
				// 重复启动只聚焦已有窗口，不起第二个进程/服务（issue #15）。
				ctx := app.Context()
				if ctx != nil {
					runtime.WindowUnminimise(ctx)
					runtime.WindowShow(ctx)
				}
			},
		},
		Bind: []interface{}{app},
	})
	if err != nil {
		msg := strings.ToLower(err.Error())
		if strings.Contains(msg, "webview") || strings.Contains(msg, "edge") {
			log.Printf("本程序需要 Microsoft Edge WebView2 运行时。请先安装：https://developer.microsoft.com/microsoft-edge/webview2/（原始错误：%v）", err)
		} else {
			log.Printf("desktop: %v", err)
		}
		os.Exit(1)
	}
}
