// Package webassets 把前端构建产物打进二进制。
//
// 与 RFERP 的做法一致：单文件部署，评委不需要另起一个前端进程。
// 开发期用 `npm run dev`（Vite 代理 /api 到 :8090），构建后覆盖 web/dist。
package webassets

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var embedded embed.FS

// FS 返回 dist 子目录，直接交给 http.FileServer。
func FS() fs.FS {
	sub, err := fs.Sub(embedded, "dist")
	if err != nil {
		// dist 是编译期嵌入的，取不到只可能是构建产物缺失。
		panic("webassets: dist not embedded: " + err.Error())
	}
	return sub
}
