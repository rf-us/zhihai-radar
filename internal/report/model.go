// Package report 定义调研报告结构与 PDF 渲染。
//
// 结构定义与 docs/report-framework.md 一致：同一份 Document 同时服务屏幕与 PDF，
// 避免两处漂移。#7 先用假数据把版式调通，#8 接真数据时直接复用这些类型。
package report

// Document 是一份完整调研报告。
type Document struct {
	Title    string
	Meta     Meta
	Sections []Section
}

// Meta 是可回溯的元信息：评委最常问"这是什么时候的数据"。
type Meta struct {
	GeneratedAt string // ISO 时间，注入的可复现时间戳
	Keyword     string
	Market      string
	Source      string // 数据来源说明（知乎开放平台 / 离线快照）
	SnapshotAt  string // 数据快照时间
}

// Section 是一节内容。先给判断（Lead），再给支撑材料。
type Section struct {
	Key         string // summary / demand / competition / cost / supply / risk / compliance ...
	Heading     string
	Lead        string
	Bullets     []string
	Table       *Table
	Evidence    []EvidenceRef
	Conditional bool // 由问答顾虑触发的条件章节
}

// Table 是简单表格：首行是表头，跨页时复制表头。
type Table struct {
	Headers []string
	Rows    [][]string
}

// EvidenceRef 可回溯到 opportunity_evidence.id 与原文链接。
type EvidenceRef struct {
	Title       string
	URL         string
	CollectedAt string
	Summary     string
}
