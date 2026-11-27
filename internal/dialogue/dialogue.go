// Package dialogue 是"限制性问答"的题库与推导规则。
//
// 主界面不是搜索框，而是六道选择题：卖家知道自己在卖什么，但不知道该问什么；
// 评委也不知道能试什么。把入口收窄，同时让每个答案都直接映射到检索参数或
// 报告章节——问完就能生成，不需要额外配置。
package dialogue

import (
	"errors"
	"fmt"
	"strings"
)

type Option struct {
	Value string `json:"value"`
	Label string `json:"label"`
	Hint  string `json:"hint,omitempty"`
	// Category / Keyword / PriceUSD 等是推导用的元数据，不下发前端。
	Category string   `json:"-"`
	Keyword  string   `json:"-"`
	PriceUSD *float64 `json:"-"`
}

type Question struct {
	Key      string   `json:"key"`
	Title    string   `json:"title"`
	Prompt   string   `json:"prompt"`
	Multiple bool     `json:"multiple"`
	Options  []Option `json:"options"`
	// FreeText 允许在选项之外自己填（仅 Q1）。
	FreeText bool `json:"free_text"`
}

func f(v float64) *float64 { return &v }

// Questions 返回固定题库。顺序即界面顺序，不要随意调整——
// 报告章节的触发依赖 Q5 的取值。
func Questions() []Question {
	return []Question{
		{
			Key: "direction", Title: "品类方向", Prompt: "你这次想看哪个方向？", FreeText: true,
			Options: []Option{
				{Value: "coffee", Label: "便携咖啡机", Hint: "家居小家电", Category: "家居小家电", Keyword: "便携咖啡机"},
				{Value: "lamp", Label: "折叠露营灯", Hint: "户外装备", Category: "户外装备", Keyword: "折叠露营灯"},
				{Value: "pet", Label: "宠物饮水机", Hint: "宠物用品", Category: "宠物用品", Keyword: "宠物饮水机"},
				{Value: "massage", Label: "筋膜枪", Hint: "个护健康", Category: "个护健康", Keyword: "筋膜枪"},
			},
		},
		{
			Key: "market", Title: "目标市场", Prompt: "主要卖到哪个市场？",
			Options: []Option{
				{Value: "na", Label: "北美"}, {Value: "eu", Label: "欧洲"},
				{Value: "jp", Label: "日本"}, {Value: "sea", Label: "东南亚"},
			},
		},
		{
			Key: "price", Title: "目标零售价", Prompt: "打算卖多少钱？",
			Options: []Option{
				{Value: "usd_lt30", Label: "低于 $30", PriceUSD: f(25)},
				{Value: "usd_30_80", Label: "$30 – 80", PriceUSD: f(55)},
				{Value: "usd_80_150", Label: "$80 – 150", PriceUSD: f(115)},
				{Value: "usd_gt150", Label: "高于 $150", PriceUSD: f(180)},
			},
		},
		{
			Key: "supply", Title: "供应链基础", Prompt: "工厂这边是什么情况？",
			Options: []Option{
				{Value: "own", Label: "自有工厂"}, {Value: "oem", Label: "长期代工"},
				{Value: "none", Label: "还没有固定供应商"},
			},
		},
		{
			Key: "concerns", Title: "主要顾虑", Prompt: "最担心哪几件事？（可多选）", Multiple: true,
			Options: []Option{
				{Value: "competition", Label: "竞争太卷"},
				{Value: "certification", Label: "认证门槛"},
				{Value: "logistics", Label: "物流成本"},
				{Value: "aftersales", Label: "售后与退换"},
			},
		},
		{
			Key: "depth", Title: "验证深度", Prompt: "这次想看到什么程度？",
			Options: []Option{
				{Value: "quick", Label: "先看结论", Hint: "约 4 页"},
				{Value: "full", Label: "完整证据链", Hint: "约 10–14 页"},
			},
		},
	}
}

// Answers 是 key → 选中的 value（多选用逗号分隔）。
type Answers map[string]string

type Plan struct {
	Keyword     string   `json:"keyword"`
	Category    string   `json:"category"`
	Market      string   `json:"market"`
	TargetUSD   *float64 `json:"target_usd"`
	TargetCNY   *float64 `json:"target_cny"`
	SupplyBase  string   `json:"supply_base"`
	Concerns    []string `json:"concerns,omitempty"`
	Depth       string   `json:"depth"`
	EvidenceMax int      `json:"evidence_max"`
	Sections    []string `json:"sections,omitempty"`
}

// USDToCNY 只是报告与测算的展示换算，不参与任何网络调用。
// 取值写死是刻意的：汇率波动不该让同一份报告两次跑出不同数字。
const USDToCNY = 7.2

var ErrMissing = errors.New("dialogue: 必答题未完成")

// Derive 把答案翻译成检索与报告参数。
func Derive(a Answers) (Plan, error) {
	var p Plan
	qs := Questions()

	// 第一题：选项或自填
	direction := strings.TrimSpace(a["direction"])
	if direction == "" {
		return p, fmt.Errorf("%w: %s", ErrMissing, qs[0].Title)
	}
	matched := false
	for _, o := range qs[0].Options {
		if o.Value == direction {
			p.Keyword, p.Category = o.Keyword, o.Category
			matched = true
			break
		}
	}
	if !matched {
		// 自填：当作关键词，品类留空
		p.Keyword = direction
	}
	if p.Keyword == "" {
		return p, fmt.Errorf("%w: %s", ErrMissing, qs[0].Title)
	}

	if p.Market = a["market"]; p.Market == "" {
		return p, fmt.Errorf("%w: %s", ErrMissing, qs[1].Title)
	}

	price := a["price"]
	if price == "" {
		return p, fmt.Errorf("%w: %s", ErrMissing, qs[2].Title)
	}
	for _, o := range qs[2].Options {
		if o.Value == price && o.PriceUSD != nil {
			usd := *o.PriceUSD
			p.TargetUSD = &usd
			cny := usd * USDToCNY
			p.TargetCNY = &cny
		}
	}
	if p.TargetUSD == nil {
		return p, fmt.Errorf("未知的价格区间 %q", price)
	}

	if p.SupplyBase = a["supply"]; p.SupplyBase == "" {
		return p, fmt.Errorf("%w: %s", ErrMissing, qs[3].Title)
	}

	for _, c := range strings.Split(a["concerns"], ",") {
		c = strings.TrimSpace(c)
		if c != "" {
			p.Concerns = append(p.Concerns, c)
		}
	}

	p.Depth = a["depth"]
	if p.Depth == "" {
		p.Depth = "quick"
	}
	if p.Depth == "full" {
		p.EvidenceMax = 30
	} else {
		p.EvidenceMax = 8
	}

	p.Sections = conditionalSections(p.Concerns)
	return p, nil
}

// conditionalSections 是"报告随问答变化"的落点：选了才生成，没选就不出现。
func conditionalSections(concerns []string) []string {
	m := map[string]string{
		"certification": "compliance",
		"logistics":     "logistics",
		"aftersales":    "aftersales",
	}
	var out []string
	seen := map[string]bool{}
	for _, c := range concerns {
		if sec, ok := m[c]; ok && !seen[sec] {
			seen[sec] = true
			out = append(out, sec)
		}
	}
	return out
}
