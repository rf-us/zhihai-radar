package dialogue

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Session 是一次开放式对话的状态。
//
// 与 Questions 的关系：题库仍是"兜底与规范"，但用户不必按题作答。
// 每一轮从自由文本里抽取槽位，缺什么问什么，抽不到就只问一句、不追问第二遍。
//
// 边界写死在这里：**与选品无关的输入不回答**，只把话题拉回来。
// 这是产品要求，也是安全边界——它不是一个通用聊天机器人。
type Session struct {
	slots Slots
}

type Slots struct {
	Keyword    string
	Category   string
	Market     string
	PriceBand  string
	SupplyBase string
	Concerns   []string
	Depth      string
}

type Reply struct {
	Kind    string   `json:"kind"` // question | ready | refused
	Text    string   `json:"text"`
	Missing []string `json:"missing,omitempty"`
	Plan    *Plan    `json:"plan,omitempty"`
}

const (
	KindQuestion = "question"
	KindReady    = "ready"
	KindRefused  = "refused"
)

// 与选品相关的词。用来判断"该不该接这句话"，而不是用来理解语义。
var domainWords = []string{
	"选品", "品类", "产品", "卖", "出海", "跨境", "市场", "价格", "定价", "成本",
	"供应商", "工厂", "代工", "采购", "毛利", "竞品", "认证", "物流", "报关",
	"咖啡", "露营", "宠物", "筋膜", "小家电", "户外", "个护",
}

var categoryHints = []struct {
	words    []string
	keyword  string
	category string
}{
	{[]string{"咖啡"}, "便携咖啡机", "家居小家电"},
	{[]string{"露营", "营地", "户外"}, "折叠露营灯", "户外装备"},
	{[]string{"宠物", "猫", "狗"}, "宠物饮水机", "宠物用品"},
	{[]string{"筋膜", "按摩"}, "筋膜枪", "个护健康"},
	{[]string{"小家电", "家电"}, "小家电", "家居小家电"},
}

var marketHints = []struct {
	words []string
	value string
}{
	{[]string{"北美", "美国", "加拿大", "amazon us"}, "na"},
	{[]string{"欧洲", "欧盟", "德国", "法国", "英国"}, "eu"},
	{[]string{"日本", "东京", "jp"}, "jp"},
	{[]string{"东南亚", "越南", "泰国", "印尼", "马来"}, "sea"},
}

var supplyHints = []struct {
	words []string
	value string
}{
	{[]string{"自有工厂", "自己的厂", "自己厂", "工厂是我"}, "own"},
	{[]string{"代工", "合作工厂", "长期合作"}, "oem"},
	{[]string{"没有供应商", "没供应商", "还没找", "无固定", "没有固定供应商", "没固定供应商"}, "none"},
}

var concernHints = []struct {
	words []string
	value string
}{
	{[]string{"竞争", "卷", "红海"}, "competition"},
	{[]string{"认证", "ce", "fcc", "合规"}, "certification"},
	{[]string{"物流", "运费", "头程", "海运"}, "logistics"},
	{[]string{"售后", "退货", "维修"}, "aftersales"},
}

var priceRe = regexp.MustCompile(`(?i)(?:\$|usd|美元)?\s*(\d+(?:\.\d+)?)\s*(?:-|~|–|到|至)?\s*(\d+(?:\.\d+)?)?\s*(?:美元|美金|usd|\$)?`)
var priceOnlyRe = regexp.MustCompile(`^(?:大概\s*)?\d+(?:\.\d+)?(?:\s*(?:-|~|–|到|至)\s*\d+(?:\.\d+)?)?\s*(?:左右)?$`)
var explicitDirection = regexp.MustCompile(`品类(?:方向)?\s*(?:是|[:：=])\s*([^，,。；;\r\n]+)`)

func (s *Session) Turn(input string) Reply {
	text := strings.TrimSpace(input)
	if text == "" {
		return Reply{Kind: KindQuestion, Text: "说说你这次想看的品类？"}
	}

	beforeKey := s.slots.Keyword + "|" + s.slots.Market + "|" + s.slots.PriceBand + "|" + s.slots.SupplyBase + "|" + s.slots.Depth
	beforeConcerns := len(s.slots.Concerns)
	s.extract(text)
	filled := beforeKey != s.slots.Keyword+"|"+s.slots.Market+"|"+s.slots.PriceBand+"|"+s.slots.SupplyBase+"|"+s.slots.Depth ||
		len(s.slots.Concerns) != beforeConcerns

	// 一句都没抽到，且看不出与选品有关 → 明确不接。
	if !filled && !looksOnTopic(text) {
		return Reply{
			Kind: KindRefused,
			Text: "这个和选品无关，我就不展开了。我们回到正题——你这次想看哪个品类，卖到哪个市场？",
		}
	}

	return s.Reply()
}

func (s *Session) Reply() Reply {
	if missing := s.missing(); len(missing) > 0 {
		return Reply{
			Kind:    KindQuestion,
			Text:    s.ack() + s.ask(missing[0]),
			Missing: missing,
		}
	}

	plan, err := Derive(s.Answers())
	if err != nil {
		return Reply{Kind: KindQuestion, Text: "还差一点信息：" + err.Error()}
	}
	plan.Category = s.slots.Category
	return Reply{
		Kind: KindReady,
		Text: fmt.Sprintf("信息齐了：%s，目标市场 %s，价格带 %s。可以开始分析。",
			plan.Keyword, marketLabel(plan.Market), priceLabel(s.slots.PriceBand)),
		Plan: &plan,
	}
}

func (s *Session) Update(key, value string) error {
	value = strings.TrimSpace(value)
	for _, question := range Questions() {
		if question.Key != key {
			continue
		}
		if question.FreeText {
			if utf8.RuneCountInString(value) > 40 {
				return fmt.Errorf("品类词最多 40 个字符")
			}
			s.slots.Keyword, s.slots.Category = value, ""
			for _, option := range question.Options {
				if value == option.Value || value == option.Keyword {
					s.slots.Keyword, s.slots.Category = option.Keyword, option.Category
					break
				}
			}
			return nil
		}

		var values []string
		if value != "" {
			for _, item := range strings.Split(value, ",") {
				valid := false
				for _, option := range question.Options {
					if item == option.Value {
						valid = true
						break
					}
				}
				if !valid || contains(values, item) || (!question.Multiple && len(values) > 0) {
					return fmt.Errorf("%s的选项无效", question.Title)
				}
				values = append(values, item)
			}
		}
		switch key {
		case "market":
			s.slots.Market = value
		case "price":
			s.slots.PriceBand = value
		case "supply":
			s.slots.SupplyBase = value
		case "concerns":
			s.slots.Concerns = values
		case "depth":
			s.slots.Depth = value
		}
		return nil
	}
	return fmt.Errorf("未知槽位")
}

func (s *Session) extract(text string) {
	lower := strings.ToLower(text)

	if s.slots.Keyword == "" {
		if match := explicitDirection.FindStringSubmatch(text); len(match) != 0 {
			_ = s.Update("direction", match[1])
		}
		if s.slots.Keyword == "" {
			for _, h := range categoryHints {
				if containsAny(lower, h.words) {
					s.slots.Keyword, s.slots.Category = h.keyword, h.category
					break
				}
			}
		}
	}
	if s.slots.Market == "" {
		for _, h := range marketHints {
			if containsAny(lower, h.words) {
				s.slots.Market = h.value
				break
			}
		}
	}
	if s.slots.SupplyBase == "" {
		for _, h := range supplyHints {
			if containsAny(lower, h.words) {
				s.slots.SupplyBase = h.value
				break
			}
		}
	}
	if s.slots.PriceBand == "" && (looksOnTopic(lower) || priceOnlyRe.MatchString(lower) ||
		containsAny(lower, []string{"$", "美元", "美金", "usd"})) {
		if band := detectPrice(lower); band != "" {
			s.slots.PriceBand = band
		}
	}
	for _, h := range concernHints {
		if containsAny(lower, h.words) && !contains(s.slots.Concerns, h.value) {
			s.slots.Concerns = append(s.slots.Concerns, h.value)
		}
	}
	if s.slots.Depth == "" {
		switch {
		case containsAny(lower, []string{"先看结论", "快速看"}):
			s.slots.Depth = "quick"
		case containsAny(lower, []string{"完整", "详细", "证据链", "报告"}):
			s.slots.Depth = "full"
		}
	}
}

func (s *Session) missing() []string {
	var out []string
	if s.slots.Keyword == "" {
		out = append(out, "direction")
	}
	if s.slots.Market == "" {
		out = append(out, "market")
	}
	if s.slots.PriceBand == "" {
		out = append(out, "price")
	}
	if s.slots.SupplyBase == "" {
		out = append(out, "supply")
	}
	return out
}

func (s *Session) ask(slot string) string {
	switch slot {
	case "direction":
		return "先说品类：你这次想看什么？比如便携咖啡机、折叠露营灯。"
	case "market":
		return "主要卖到哪个市场？北美、欧洲、日本还是东南亚？"
	case "price":
		return "打算卖多少钱？低于 $30、$30–80、$80–150，还是更高？"
	case "supply":
		return "工厂这边什么情况：自有工厂、长期代工，还是还没有固定供应商？"
	}
	return "还想补充点什么？"
}

func (s *Session) ack() string {
	if s.slots.Keyword != "" && s.slots.Market != "" {
		return "好，" + s.slots.Keyword + "、卖" + marketLabel(s.slots.Market) + "。"
	}
	if s.slots.Keyword != "" {
		return "好，" + s.slots.Keyword + "。"
	}
	return ""
}

func (s *Session) Answers() Answers {
	a := Answers{"direction": s.slots.Keyword, "market": s.slots.Market,
		"price": s.slots.PriceBand, "supply": s.slots.SupplyBase}
	if len(s.slots.Concerns) > 0 {
		a["concerns"] = strings.Join(s.slots.Concerns, ",")
	}
	if s.slots.Depth != "" {
		a["depth"] = s.slots.Depth
	}
	return a
}

// detectPrice 把"$50""三十美元""100 左右"归到固定价格带。
// 归到带而不是记具体数字：报告与 D5 用带的中值，避免同一份材料两次跑出不同结论。
func detectPrice(text string) string {
	m := priceRe.FindStringSubmatch(text)
	if m == nil {
		return ""
	}
	v, err := strconv.ParseFloat(m[1], 64)
	if err != nil || v <= 0 {
		return ""
	}
	if m[2] != "" {
		if hi, err := strconv.ParseFloat(m[2], 64); err == nil && hi > v {
			v = (v + hi) / 2
		}
	}
	switch {
	case v < 30:
		return "usd_lt30"
	case v < 80:
		return "usd_30_80"
	case v < 150:
		return "usd_80_150"
	default:
		return "usd_gt150"
	}
}

func looksOnTopic(text string) bool {
	lower := strings.ToLower(text)
	for _, w := range domainWords {
		if strings.Contains(lower, w) {
			return true
		}
	}
	return false
}

func containsAny(text string, words []string) bool {
	for _, w := range words {
		if strings.Contains(text, w) {
			return true
		}
	}
	return false
}

func contains(xs []string, v string) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}

func marketLabel(v string) string {
	switch v {
	case "na":
		return "北美"
	case "eu":
		return "欧洲"
	case "jp":
		return "日本"
	case "sea":
		return "东南亚"
	}
	return v
}

func priceLabel(v string) string {
	switch v {
	case "usd_lt30":
		return "低于 $30"
	case "usd_30_80":
		return "$30–80"
	case "usd_80_150":
		return "$80–150"
	case "usd_gt150":
		return "高于 $150"
	}
	return v
}
