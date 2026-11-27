// Package webassets 内嵌前端构建产物,让 radar 只要一个二进制就能跑。
//
// 目录约定(相对 internal/):
//
//	webassets/fallback.html  常驻占位页(入库)
//	webassets/dist/**        由 web/ 的 npm run build 生成(不入库)
//
// 包为什么在 internal/ 而不是 internal/webassets:
// go:embed 只能匹配当前包目录以内的文件,而且每个模式都必须至少命中一个文件。
// 要同时满足「整个 internal/webassets/dist 不存在也能 go build」和「产物仍输出到
// internal/webassets/dist」,嵌入根就必须是 dist 的父目录 internal/webassets;
// 它里面有常驻的 fallback.html,所以 all:webassets 永远有匹配,dist 可有可无。
// Go 源码放在 internal/ 而不在 webassets/ 子树内,因此不会被嵌入,更不会作为
// 静态资源暴露。
package webassets

import (
	"embed"
	"io/fs"
)

//go:embed all:webassets
var embedded embed.FS

const (
	fallbackPath = "webassets/fallback.html"
	distDir      = "webassets/dist"
)

// FS 返回构建产物视图(即 webassets/dist 本身),可直接交给 http.FileServer。
func FS() fs.FS {
	sub, err := fs.Sub(embedded, distDir)
	if err != nil {
		panic("webassets: webassets/dist not embedded: " + err.Error())
	}
	return sub
}

// Built 报告前端产物是否已打进二进制(即 webassets/dist/index.html 存在)。
func Built() bool {
	_, err := fs.Stat(embedded, distDir+"/index.html")
	return err == nil
}

// IndexHTML 返回 webassets/dist/index.html 的内容;未构建时 ok 为 false。
func IndexHTML() (body []byte, ok bool) {
	b, err := embedded.ReadFile(distDir + "/index.html")
	if err != nil {
		return nil, false
	}
	return b, true
}

// Fallback 是前端未构建时展示的占位页,随二进制常驻。
func Fallback() []byte {
	b, err := embedded.ReadFile(fallbackPath)
	if err != nil {
		// fallback.html 是入库文件,取不到只可能是嵌入配置被改坏。
		panic("webassets: fallback page not embedded: " + err.Error())
	}
	return b
}
