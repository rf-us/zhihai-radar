package webassets

import (
	"io/fs"
	"strings"
	"testing"
)

// 占位页必须始终可嵌入:没有它,未构建前端的机器就只剩编译失败一条路。
func TestFallbackAlwaysEmbedded(t *testing.T) {
	if len(Fallback()) == 0 {
		t.Fatal("fallback page is empty; go:embed all:webassets failed")
	}
}

// IndexHTML 与 Built 必须一致:两者都只看 webassets/dist/index.html 是否存在。
func TestBuiltMatchesIndex(t *testing.T) {
	body, ok := IndexHTML()
	if ok != Built() {
		t.Fatalf("Built()=%v but IndexHTML ok=%v", Built(), ok)
	}
	if ok && len(body) == 0 {
		t.Fatal("IndexHTML reported ok but returned empty body")
	}
}

// 嵌入根必须只含站点数据:Go 源码和其它 internal 包都不能被带进二进制,
// 更不能作为静态资源暴露。
func TestEmbeddedOnlyContainsSiteData(t *testing.T) {
	entries, err := fs.ReadDir(embedded, ".")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "webassets" {
		t.Fatalf("embedded root should only contain webassets, got %v", entries)
	}

	err = fs.WalkDir(embedded, "webassets", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(p, ".go") {
			t.Errorf("Go source embedded as static data: %s", p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fs.Stat(embedded, fallbackPath); err != nil {
		t.Fatalf("fallback not embedded: %v", err)
	}
}
