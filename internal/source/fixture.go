package source

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Dataset 是一次完整分析的离线快照：四路数据各一份。
type Dataset struct {
	Note    string `json:"_note"`
	Keyword string `json:"keyword"`
	Zhihu   []Post `json:"zhihu"`
	Global  []Post `json:"global"`
	Hot     []Post `json:"hot"`
	Answer  string `json:"answer"`
	// CollectedAt 是快照采集日期（ISO 日期或时间），报告里展示数据时效。
	CollectedAt string `json:"collected_at"`
}

// Fixture 回放离线快照。
//
// 存在的意义不只是测试：知乎热榜与直答各 100 次/天，演示现场点几次就没了。
// 离线快照让公网 Demo 和答辩演示都不依赖实时配额。
type Fixture struct {
	data map[string]Dataset
}

func NewFixture(datasets ...Dataset) *Fixture {
	m := make(map[string]Dataset, len(datasets))
	for _, d := range datasets {
		m[normalize(d.Keyword)] = d
	}
	return &Fixture{data: m}
}

// LoadFixtureDir 读取目录下所有 *.json 快照，按 keyword 建索引。
func LoadFixtureDir(dir string) (*Fixture, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var all []Dataset
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		var d Dataset
		if err := json.Unmarshal(raw, &d); err != nil {
			return nil, fmt.Errorf("fixture %s: %w", e.Name(), err)
		}
		// 缺字段时必须指出文件名与具体字段，不能只说 invalid json（issue #3 验收）。
		if strings.TrimSpace(d.Keyword) == "" {
			return nil, fmt.Errorf("fixture %s: 缺少必填字段 keyword", e.Name())
		}
		if d.Zhihu == nil {
			return nil, fmt.Errorf("fixture %s: 缺少必填字段 zhihu（品类 %s）", e.Name(), d.Keyword)
		}
		if d.Global == nil {
			return nil, fmt.Errorf("fixture %s: 缺少必填字段 global（品类 %s）", e.Name(), d.Keyword)
		}
		if d.Hot == nil {
			return nil, fmt.Errorf("fixture %s: 缺少必填字段 hot（品类 %s）", e.Name(), d.Keyword)
		}
		if strings.TrimSpace(d.Answer) == "" {
			return nil, fmt.Errorf("fixture %s: 缺少必填字段 answer（品类 %s）", e.Name(), d.Keyword)
		}
		all = append(all, d)
	}
	return NewFixture(all...), nil
}

func (f *Fixture) SearchZhihu(_ context.Context, keyword string, count int) ([]Post, error) {
	d, err := f.get(keyword)
	if err != nil {
		return nil, err
	}
	return limit(tag(d.Zhihu, "zhihu_search"), count), nil
}

func (f *Fixture) SearchGlobal(_ context.Context, keyword string, count int) ([]Post, error) {
	d, err := f.get(keyword)
	if err != nil {
		return nil, err
	}
	return limit(tag(d.Global, "global_search"), count), nil
}

func (f *Fixture) Hot(_ context.Context, limitN int) ([]Post, error) {
	// 热榜与关键词无关：把所有快照的 hot 合并去重即可。
	seen := map[string]bool{}
	var out []Post
	for _, d := range f.data {
		for _, p := range d.Hot {
			if seen[p.URL] {
				continue
			}
			seen[p.URL] = true
			out = append(out, p)
		}
	}
	return limit(tag(out, "hot"), limitN), nil
}

func (f *Fixture) Ask(_ context.Context, _ string) (string, error) {
	// fixture 模式下由调用方按关键词取答案；这里返回第一个非空答案兜底。
	for _, d := range f.data {
		if strings.TrimSpace(d.Answer) != "" {
			return d.Answer, nil
		}
	}
	return `{"pain_points":[]}`, nil
}

// AnswerFor 让 service 在 fixture 模式下取到与关键词匹配的答案。
func (f *Fixture) AnswerFor(keyword string) string {
	if d, err := f.get(keyword); err == nil {
		return d.Answer
	}
	return ""
}

func (f *Fixture) get(keyword string) (Dataset, error) {
	if d, ok := f.data[normalize(keyword)]; ok {
		return d, nil
	}
	if len(f.data) == 1 {
		for _, d := range f.data {
			return d, nil
		}
	}
	return Dataset{}, fmt.Errorf("%w: 没有关键词 %q 的离线快照", ErrUnavailable, keyword)
}

func tag(posts []Post, source string) []Post {
	out := make([]Post, len(posts))
	for i, p := range posts {
		p.Source = source
		out[i] = p
	}
	return out
}

func limit(posts []Post, n int) []Post {
	if n <= 0 || n > len(posts) {
		return posts
	}
	return posts[:n]
}

func normalize(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// Keywords 返回快照里所有可用的品类关键词（稳定排序），供命令行 -list 展示。
func (f *Fixture) Keywords() []string {
	out := make([]string, 0, len(f.data))
	for k := range f.data {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// DatasetOf 返回某关键词对应的快照（含采集日期与来源说明），供展示与诊断。
func (f *Fixture) DatasetOf(keyword string) (Dataset, error) {
	return f.get(keyword)
}
