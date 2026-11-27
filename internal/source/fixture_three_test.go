package source

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestThreeFixturesLoadAndProduceEvidence(t *testing.T) {
	dir := filepath.Join("..", "..", "fixtures")
	f, err := LoadFixtureDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	keywords := f.Keywords()
	if len(keywords) != 3 {
		t.Fatalf("应有 3 个品类，实际 %d：%v", len(keywords), keywords)
	}
	want := map[string]bool{"便携咖啡机": true, "折叠露营灯": true, "宠物饮水机": true}
	for _, k := range keywords {
		if !want[k] {
			t.Fatalf("意外品类：%s", k)
		}
		d, err := f.DatasetOf(k)
		if err != nil {
			t.Fatal(err)
		}
		if d.Note == "" || d.CollectedAt == "" {
			t.Logf("提示：%s 未标注采集日期字段（note=%q collected=%q）", k, d.Note, d.CollectedAt)
		}
		zhihu, err := f.SearchZhihu(context.Background(), k, 10)
		if err != nil || len(zhihu) == 0 {
			t.Fatalf("%s 知乎证据为空：%v", k, err)
		}
		global, _ := f.SearchGlobal(context.Background(), k, 20)
		if len(global) == 0 {
			t.Fatalf("%s 全网证据为空", k)
		}
		answer := f.AnswerFor(k)
		if answer == "" {
			t.Fatalf("%s 直答为空", k)
		}
	}
}

func writeFileForTest(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o600)
}

func TestFixtureMissingFieldNamesFileAndField(t *testing.T) {
	dir := t.TempDir()
	// 写一个缺 keyword 的快照，错误信息应包含文件名与字段名。
	path := filepath.Join(dir, "broken.json")
	if err := writeFileForTest(path, `{"zhihu":[],"global":[],"hot":[],"answer":"{}"}`); err != nil {
		t.Fatal(err)
	}
	_, err := LoadFixtureDir(dir)
	if err == nil {
		t.Fatal("缺字段应报错")
	}
	msg := err.Error()
	if !containsAll(msg, "broken.json", "keyword") {
		t.Fatalf("错误应指出文件名与字段：%s", msg)
	}
}

func containsAll(s string, parts ...string) bool {
	for _, p := range parts {
		if !strings.Contains(s, p) {
			return false
		}
	}
	return true
}
