package report

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/signintech/gopdf"
)

// 字体与子集字符清单随二进制嵌入：评委机器不需要装字体，也不需要 Python。
//
//go:embed assets/font/NotoSansSC-Regular.ttf
var fontBytes []byte

//go:embed assets/font/subset-chars.txt
var subsetChars string

const (
	family = "noto"

	pageW = 595.0 // A4 pt
	pageH = 842.0

	marginL = 51.0 // 18mm ≈ 51pt
	marginR = 51.0
	marginT = 60.0
	marginB = 56.0

	bodySize = 10.5
	lineH    = 16.0
	paraGap  = 8.0
)

// RenderResult 是渲染产物与缺字告警。
type RenderResult struct {
	PDF []byte

	// MissingRunes 是被替换成占位符的字符及次数（子集外字符）。
	MissingRunes map[rune]int
	// MissingCount 是缺字总数，写进报告末尾告警。
	MissingCount int
	// Pages 是实际页数。
	Pages int
}

// Render 把 Document 渲染为 PDF 字节。
//
// 可复现性：渲染器不读时钟；生成时间完全来自 doc.Meta.GeneratedAt。
// 同一 Document 渲染两次，除注入的时间戳外字节一致。
func Render(doc Document) (RenderResult, error) {
	var out RenderResult
	if err := validate(doc); err != nil {
		return out, err
	}

	// 缺字兜底：先把整份文档的句子替换掉，统计缺字。
	cleaned, missing := substituteMissing(doc)
	out.MissingRunes = missing
	for _, n := range missing {
		out.MissingCount += n
	}

	pdf := new(gopdf.GoPdf)
	pdf.Start(gopdf.Config{PageSize: *gopdf.PageSizeA4})
	if err := pdf.AddTTFFontData(family, fontBytes); err != nil {
		return out, fmt.Errorf("嵌入中文字体失败：%w", err)
	}
	if err := pdf.SetFont(family, "", bodySize); err != nil {
		return out, fmt.Errorf("设置字体失败：%w", err)
	}

	r := &runner{pdf: pdf, doc: cleaned}
	r.addPage() // 首页（页眉在 addPage 里画）

	// 封面区：标题 + 元信息（页眉只留品类 + 日期，符合砍单后的朴素版式）。
	r.drawCover()

	for _, sec := range cleaned.Sections {
		r.ensureSpace(lineH * 4)
		r.drawHeading(sec.Heading)
		if sec.Lead != "" {
			r.drawParagraph(sec.Lead)
		}
		for _, b := range sec.Bullets {
			r.drawBullet(b)
		}
		if sec.Table != nil {
			r.drawTable(*sec.Table)
		}
		for _, ev := range sec.Evidence {
			r.drawEvidence(ev)
		}
		if r.y > marginB+lineH {
			r.y += paraGap
		}
	}

	// 缺字告警（issue #7 要求文档末尾输出）。
	if out.MissingCount > 0 {
		r.ensureSpace(lineH * 2)
		r.drawParagraphPlain(fmt.Sprintf("⚠ 有 %d 个字符未在当前字体子集中渲染，已显示为 □。", out.MissingCount))
	}

	r.drawFooter() // 最后一页页脚

	var buf bytes.Buffer
	if _, err := pdf.WriteTo(&buf); err != nil {
		return out, fmt.Errorf("生成 PDF 失败：%w", err)
	}
	out.PDF = buf.Bytes()
	out.Pages = r.pages
	return out, nil
}

// validate 检查必要字段，避免生成一份没有标题与时间的空报告。
func validate(doc Document) error {
	if strings.TrimSpace(doc.Title) == "" {
		return errors.New("报告标题不能为空")
	}
	if strings.TrimSpace(doc.Meta.GeneratedAt) == "" {
		return errors.New("生成时间不能为空（可复现性要求由调用方注入）")
	}
	if len(doc.Sections) == 0 {
		return errors.New("报告至少需要一节")
	}
	return nil
}

// runner 持有分页状态与当前 y 坐标。
type runner struct {
	pdf     *gopdf.GoPdf
	doc     Document
	y       float64
	pages   int
	missing int
}

// addPage 开新页并画页眉。gopdf 的 Start() 不隐式建首页，必须显式 AddPage()；
// 漏掉这一步时写文本会解引用空 content 而 panic（实测所有字体都崩）。
func (r *runner) addPage() {
	r.pdf.AddPage()
	r.pages++
	r.pdf.SetXY(marginL, 30)
	_ = r.pdf.SetFontSize(8)
	_ = r.cell(fmt.Sprintf("%s · %s", r.doc.Title, r.doc.Meta.GeneratedAt))
	_ = r.pdf.SetFontSize(bodySize)
	r.y = marginT
}

// drawFooter 在当前页底部写页码。调用时机：换页前与全文收尾，均对应当前 r.pages。
func (r *runner) drawFooter() {
	r.pdf.SetXY(marginL, pageH-28)
	_ = r.pdf.SetFontSize(8)
	_ = r.cell(fmt.Sprintf("知海选品雷达 · 生成于 %s · 第 %d 页", r.doc.Meta.GeneratedAt, r.pages))
	_ = r.pdf.SetFontSize(bodySize)
}

func (r *runner) ensureSpace(need float64) {
	if r.y+need <= pageH-marginB {
		return
	}
	r.drawFooter()
	r.addPage()
}

func (r *runner) setFont(size float64) {
	_ = r.pdf.SetFontSize(size)
}

// cell 用真实 Rect 写单行文本。gopdf 子集字体路径下 Cell(nil,...) 会解引用
// 空矩形而 panic，必须给有尺寸的 Rect；超宽文本由 truncate 提前截断。
func (r *runner) cell(text string) error {
	return r.pdf.Cell(&gopdf.Rect{W: pageW - marginL - marginR, H: lineH}, text)
}

// drawCover 画首页标题区。砍单后不画目录与指标卡，只保留标题与可回溯元信息。
func (r *runner) drawCover() {
	r.setFont(20)
	r.pdf.SetXY(marginL, r.y+10)
	_ = r.cell(truncate(r.doc.Title, 32))
	r.y += lineH * 2.2

	r.setFont(10)
	lines := []string{}
	if r.doc.Meta.Keyword != "" {
		lines = append(lines, "关键词："+r.doc.Meta.Keyword)
	}
	if r.doc.Meta.Market != "" {
		lines = append(lines, "目标市场："+r.doc.Meta.Market)
	}
	if r.doc.Meta.Source != "" {
		lines = append(lines, "数据来源："+r.doc.Meta.Source)
	}
	if r.doc.Meta.SnapshotAt != "" {
		lines = append(lines, "数据快照："+r.doc.Meta.SnapshotAt)
	}
	for _, line := range lines {
		r.pdf.SetXY(marginL, r.y)
		_ = r.cell(truncate(line, 70))
		r.y += lineH
	}
	r.y += paraGap
}

func (r *runner) drawHeading(text string) {
	r.setFont(14)
	r.pdf.SetXY(marginL, r.y)
	_ = r.cell(truncate(text, 60))
	r.y += lineH + 4
	r.setFont(bodySize)
}

func (r *runner) drawParagraph(text string) {
	r.setFont(bodySize)
	r.pdf.SetXY(marginL, r.y)
	rect := &gopdf.Rect{W: pageW - marginL - marginR, H: lineH * 4}
	_ = r.pdf.MultiCell(rect, text)
	// MultiCell 从当前 y 往下排，粗略按行数推进（每行约 lineH）。
	lines := estimateLines(text, rect.W, bodySize)
	r.y += float64(lines)*lineH + paraGap
}

func (r *runner) drawParagraphPlain(text string) {
	r.drawParagraph(text)
}

func (r *runner) drawBullet(text string) {
	r.ensureSpace(lineH)
	r.setFont(bodySize)
	r.pdf.SetXY(marginL, r.y)
	_ = r.cell("• " + truncate(text, 80))
	r.y += lineH
}

func (r *runner) drawEvidence(ev EvidenceRef) {
	r.ensureSpace(lineH * 2)
	r.setFont(9)
	r.pdf.SetXY(marginL+8, r.y)
	label := ev.Title
	if ev.CollectedAt != "" {
		label += "（采集于 " + ev.CollectedAt + "）"
	}
	_ = r.cell("· " + truncate(label, 70))
	r.y += lineH * 0.8
	if ev.URL != "" {
		r.pdf.SetXY(marginL+16, r.y)
		_ = r.cell(truncate(ev.URL, 78))
		r.y += lineH * 0.8
	}
	r.setFont(bodySize)
}

// drawTable 画表格：表头 + 逐行；行高按最长单元格估算，跨页时复制表头。
func (r *runner) drawTable(t Table) {
	if len(t.Headers) == 0 {
		return
	}
	cols := len(t.Headers)
	colW := (pageW - marginL - marginR) / float64(cols)
	rowLines := func(cells []string) int {
		n := 1
		for _, c := range cells {
			if v := estimateLines(c, colW-8, bodySize); v > n {
				n = v
			}
		}
		return n
	}

	writeRow := func(cells []string, header bool) {
		lines := rowLines(cells)
		h := float64(lines)*lineH + 4
		r.ensureSpace(h)
		if header {
			r.setFont(10)
		}
		x := marginL
		for _, c := range cells {
			r.pdf.SetXY(x+4, r.y+4)
			_ = r.cell(truncate(c, 40))
			x += colW
		}
		r.y += h
		r.setFont(bodySize)
	}

	writeRow(t.Headers, true)
	for _, row := range t.Rows {
		writeRow(row, false)
	}
	r.y += paraGap
}

// estimateLines 按字符宽度估算换行数（中文按 1 个字宽，ASCII 按 0.55）。
// gopdf 的 MultiCell 内部按字符宽度断行，这里只需要近似值来推进 y。
func estimateLines(text string, width float64, fontSize float64) int {
	if width <= 0 {
		return 1
	}
	unit := fontSize * 0.55
	perLine := int(width / unit)
	if perLine < 8 {
		perLine = 8
	}
	lines := 1
	col := 0
	for _, rr := range text {
		if rr == '\n' {
			lines++
			col = 0
			continue
		}
		w := 1.0
		if rr < 0x80 {
			w = 0.55
		}
		col += int(w * 10)
		if col >= perLine*10 {
			lines++
			col = 0
		}
	}
	return lines
}

func truncate(s string, maxRunes int) string {
	if utf8.RuneCountInString(s) <= maxRunes {
		return s
	}
	runes := []rune(s)
	return string(runes[:maxRunes-1]) + "…"
}

// substituteMissing 把子集外字符替换为 □ 并统计次数。
// 空白与控制字符不替换（PDF 排版需要它们）。
func substituteMissing(doc Document) (Document, map[rune]int) {
	allowed := make(map[rune]bool, len(subsetChars))
	for _, rr := range subsetChars {
		allowed[rr] = true
	}
	missing := make(map[rune]int)
	subst := func(s string) string {
		var b strings.Builder
		for _, rr := range s {
			if rr < 0x20 || rr == '\n' || rr == '\r' || rr == '\t' {
				b.WriteRune(rr)
				continue
			}
			if allowed[rr] {
				b.WriteRune(rr)
				continue
			}
			b.WriteRune('□')
			missing[rr]++
		}
		return b.String()
	}

	doc.Title = subst(doc.Title)
	doc.Meta.Keyword = subst(doc.Meta.Keyword)
	doc.Meta.Market = subst(doc.Meta.Market)
	doc.Meta.Source = subst(doc.Meta.Source)
	doc.Meta.SnapshotAt = subst(doc.Meta.SnapshotAt)
	for i := range doc.Sections {
		s := &doc.Sections[i]
		s.Heading = subst(s.Heading)
		s.Lead = subst(s.Lead)
		for j := range s.Bullets {
			s.Bullets[j] = subst(s.Bullets[j])
		}
		if s.Table != nil {
			for j := range s.Table.Headers {
				s.Table.Headers[j] = subst(s.Table.Headers[j])
			}
			for j := range s.Table.Rows {
				for k := range s.Table.Rows[j] {
					s.Table.Rows[j][k] = subst(s.Table.Rows[j][k])
				}
			}
		}
		for j := range s.Evidence {
			s.Evidence[j].Title = subst(s.Evidence[j].Title)
			s.Evidence[j].Summary = subst(s.Evidence[j].Summary)
			s.Evidence[j].URL = subst(s.Evidence[j].URL)
		}
	}
	return doc, missing
}

// MissingRunesSorted 供测试与诊断使用：按字符码点稳定排序。
func MissingRunesSorted(m map[rune]int) []rune {
	out := make([]rune, 0, len(m))
	for rr := range m {
		out = append(out, rr)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
