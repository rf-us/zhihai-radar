package report

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func sampleDoc() Document {
	return Document{
		Title: "便携咖啡机出海调研报告",
		Meta: Meta{
			GeneratedAt: "2026-11-27T12:00:00Z",
			Keyword:     "便携咖啡机",
			Market:      "北美",
			Source:      "知乎开放平台 + 离线快照",
			SnapshotAt:  "2026-11-20",
		},
		Sections: []Section{
			{
				Key: "summary", Heading: "一、结论摘要",
				Lead: "综合得分 78.4，结论为值得验证。便携咖啡机在知乎有真实讨论，清洗麻烦是主要痛点。",
				Bullets: []string{
					"需求热度：312 个赞、89 条评论，共命中 12 条讨论",
					"痛点强度：清洗麻烦、滤网难拆、体积偏大三条高频抱怨",
				},
			},
			{
				Key: "competition", Heading: "二、竞争格局",
				Lead: "海外竞品集中在 $40–$80 价格带，本地品牌占据中高端。",
				Table: &Table{
					Headers: []string{"品牌", "价格带", "卖点", "链接"},
					Rows: [][]string{
						{"AeroPress Go", "$39–$49", "便携、易清洗", "https://example.com/aeropress"},
						{"Wacaco Pipamoka", "$65–$75", "自加热、保温", "https://example.com/wacaco"},
						{"Handpresso", "$55–$70", "意式浓缩", "https://example.com/handpresso"},
					},
				},
			},
			{
				Key: "cost", Heading: "三、成本与定价",
				Lead: "单台成本 ¥89，建议售价 $49，毛利率 44%。",
				Table: &Table{
					Headers: []string{"零件", "数量", "单价", "损耗率", "小计"},
					Rows: [][]string{
						{"外壳", "1", "28.00", "3%", "28.84"},
						{"滤网", "1", "12.50", "5%", "13.13"},
						{"加热模块", "1", "36.00", "2%", "36.72"},
						{"包装", "1", "8.00", "0%", "8.00"},
					},
				},
			},
			{
				Key: "risk", Heading: "四、风险与下一步",
				Lead: "以下假设尚未验证，需要线下核实。",
				Bullets: []string{
					"核实北美 FDA 食品接触材料合规要求",
					"确认供应商 MOQ 与 45 天交期",
					"实测连续使用 30 天后的滤网寿命",
				},
				Evidence: []EvidenceRef{
					{Title: "买了三个月的真实体验", URL: "https://www.zhihu.com/question/1002/answer/2002", CollectedAt: "2026-11-20"},
				},
			},
		},
	}
}

func TestRenderProducesValidPDF(t *testing.T) {
	res, err := Render(sampleDoc())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.PDF) < 1000 {
		t.Fatalf("PDF 太小：%d 字节", len(res.PDF))
	}
	if !bytes.HasPrefix(res.PDF, []byte("%PDF-")) {
		t.Fatalf("不是 PDF：%q", res.PDF[:16])
	}
	if res.Pages < 1 {
		t.Fatal("页数至少 1")
	}
	// 诊断产物写到临时目录，便于人工 pdftotext 检查。
	out := filepath.Join(t.TempDir(), "sample.pdf")
	if err := os.WriteFile(out, res.PDF, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Logf("样例 PDF：%s（%d 字节，%d 页）", out, len(res.PDF), res.Pages)
}

func TestRenderIsReproducible(t *testing.T) {
	doc := sampleDoc()
	a, err := Render(doc)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Render(doc)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a.PDF, b.PDF) {
		t.Fatal("同一输入两次渲染字节不一致")
	}
}

func TestRenderMissingRunesAreSubstituted(t *testing.T) {
	doc := sampleDoc()
	// 生僻字与 emoji：子集外字符应被替换并计数。
	doc.Sections[0].Lead = "这个滤网有筋膜质感 🚀 但很好用"
	res, err := Render(doc)
	if err != nil {
		t.Fatal(err)
	}
	if res.MissingCount == 0 {
		t.Fatal("应该有缺字告警")
	}
	if _, ok := res.MissingRunes['🚀']; !ok {
		t.Fatalf("emoji 应被计为缺字：%+v", res.MissingRunes)
	}
}

func TestRenderRejectsInvalidInput(t *testing.T) {
	cases := []struct {
		name string
		doc  Document
	}{
		{"无标题", Document{Meta: Meta{GeneratedAt: "x"}, Sections: []Section{{Heading: "a"}}}},
		{"无时间", Document{Title: "t", Sections: []Section{{Heading: "a"}}}},
		{"无章节", Document{Title: "t", Meta: Meta{GeneratedAt: "x"}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := Render(c.doc); err == nil {
				t.Fatal("应返回错误")
			}
		})
	}
}

func TestRenderHandlesLongTableAcrossPages(t *testing.T) {
	doc := sampleDoc()
	// 生成一个 30 行的长表，强制跨页。
	rows := make([][]string, 0, 30)
	for i := 0; i < 30; i++ {
		rows = append(rows, []string{
			"零件名称" + strings.Repeat("长", 6),
			"1", "12.50", "5%", "13.13",
		})
	}
	doc.Sections[1].Table.Rows = rows
	res, err := Render(doc)
	if err != nil {
		t.Fatal(err)
	}
	if res.Pages < 2 {
		t.Fatalf("长表应跨页，实际 %d 页", res.Pages)
	}
}

// TestRenderEightPageSample 生成 8 页样例（3 张表 + 1 个跨页长表），
// 产物写到临时目录供 pdftotext / pdffonts 外部核验。
func TestRenderEightPageSample(t *testing.T) {
	doc := sampleDoc()
	// 表 1：已有一张竞品表（3 行）
	// 表 2：成本表（4 行）
	// 表 3：新增一张证据表
	doc.Sections[2].Table = &Table{
		Headers: []string{"来源", "标题", "点赞", "评论", "链接"},
		Rows: [][]string{
			{"知乎搜索", "买了三个月真实体验", "312", "89", "https://www.zhihu.com/q/1"},
			{"全网搜索", "AeroPress Go review", "512", "120", "https://example.com/aeropress"},
		},
	}
	// 长表：200 行强制跨多页（第 4 张表，跨页续排）
	rows := make([][]string, 0, 240)
	for i := 0; i < 240; i++ {
		rows = append(rows, []string{
			fmt.Sprintf("零件%02d薄膜滤网组件", i), "1", "12.50", "5%", "13.13",
		})
	}
	doc.Sections = append(doc.Sections, Section{
		Key: "appendix", Heading: "附录、完整用料明细",
		Lead:  "下表为完整用料明细，跨页续排。",
		Table: &Table{Headers: []string{"零件", "数量", "单价", "损耗率", "小计"}, Rows: rows},
	})

	res, err := Render(doc)
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(os.TempDir(), "radar-8page-sample.pdf")
	if err := os.WriteFile(out, res.PDF, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Logf("8 页样例：%s（%d 页，%d 字节）", out, res.Pages, len(res.PDF))
	if res.Pages < 8 {
		t.Fatalf("样例应至少 8 页，实际 %d 页", res.Pages)
	}
}
