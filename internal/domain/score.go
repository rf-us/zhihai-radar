// Package domain 是选品验证的核心模型：五维评分、结论规则与解释文案。
//
// 这个包刻意不依赖数据库、HTTP 与时钟（时间通过 Input.Now 传入），
// 因此可以被毫秒级地表驱动测试，也可以在答辩时单独演示"分数是怎么算出来的"。
package domain

import (
	"fmt"
	"math"
	"net/url"
	"sort"
	"strings"
	"time"
)

type Evidence struct {
	Source      string // zhihu_search | global_search | hot
	Title       string
	Snippet     string
	URL         string
	Votes       int
	Comments    int
	Authority   float64 // 0–4，用于 D1 加权
	PublishedAt *time.Time
}

type Pain struct {
	Text        string
	Severity    int // 1–5
	EvidenceURL string
}

type CostLine struct {
	PartID    int64
	PartName  string
	Qty       float64
	UnitPrice float64
	LossRate  float64
	UseMode   int // 0 每台用 N 个 / 1 每 M 台用 1 个（Qty 即 M）
}

type Input struct {
	Keyword     string
	Zhihu       []Evidence // 知乎搜索结果
	Global      []Evidence // 全网搜索结果
	HotHit      bool       // 关键词是否上了热榜
	Pains       []Pain
	Prices      []float64 // 海外竞品价格，用于建议售价与 D4
	CostLines   []CostLine
	TargetPrice *float64
	Now         time.Time
}

type Scores struct {
	Demand      float64  `json:"demand"`
	Pain        float64  `json:"pain"`
	Trend       float64  `json:"trend"`
	Competition float64  `json:"competition"`
	Cost        *float64 `json:"cost"`
	Overall     float64  `json:"overall"`
	Confidence  float64  `json:"confidence"`
}

// 权重之和为 1。维度缺失时按可用权重归一化，见 Score。
var weights = [...]float64{0.35, 0.20, 0.15, 0.10, 0.20}

type CostBreakdown struct {
	TotalCost   float64  `json:"total_cost"`
	Suggested   float64  `json:"suggested_price"`
	Margin      float64  `json:"margin"`
	Score       float64  `json:"score"`
	PriceSource string   `json:"price_source"`
	Lines       []string `json:"-"`
}

// Score 计算五维分与综合分，并返回每个维度的解释文案。
//
// 缺失的维度不参与加权，改为按可用权重归一化；Confidence 即可用权重之和。
// 这样"知乎挂了但全网搜索还在"仍然能给出一个诚实的分数，而不是把 0 当成真实得分。
func Score(in Input) (Scores, []string) {
	now := in.Now
	if now.IsZero() {
		now = time.Now()
	}
	var s Scores
	var reasons []string

	s.Demand = demandScore(in.Zhihu)
	reasons = append(reasons, fmt.Sprintf(
		"D1=%.0f：命中 %d 条，加权点赞 %.0f，加权评论 %.0f",
		s.Demand, len(in.Zhihu), weightedVotes(in.Zhihu), weightedComments(in.Zhihu)))

	s.Pain = painScore(in.Pains)
	validPains, severitySum := painStats(in.Pains)
	reasons = append(reasons, fmt.Sprintf(
		"D2=%.0f：有效痛点 %d 个，严重度合计 %d（无原文链接的不计分）",
		s.Pain, validPains, severitySum))

	s.Trend = trendScore(in.Global, in.HotHit, now)
	reasons = append(reasons, fmt.Sprintf(
		"D3=%.0f：%s；近 30 天讨论占比 %.0f%%",
		s.Trend, hotText(in.HotHit), recentRatio(in.Global, now)*100))

	s.Competition = competitionScore(in.Global, in.Prices)
	reasons = append(reasons, fmt.Sprintf(
		"D4=%.0f：竞品 %d 个，价格带离散度 %.2f（越高越不推荐）",
		s.Competition, competitorCount(in.Global), priceSpread(in.Prices)))

	costKnown := false
	if len(in.CostLines) > 0 {
		if cb, ok := EstimateCost(in.CostLines, in.TargetPrice, in.Prices); ok {
			costKnown = true
			v := cb.Score
			s.Cost = &v
			reasons = append(reasons, fmt.Sprintf(
				"D5=%.0f：单台成本 ¥%.2f，建议售价 ¥%.2f（%s），毛利 %.0f%%",
				cb.Score, cb.TotalCost, cb.Suggested, cb.PriceSource, cb.Margin*100))
		}
	}

	avail := [...]bool{
		len(in.Zhihu) > 0,
		validPains > 0,
		len(in.Global) > 0 || in.HotHit,
		len(in.Global) > 0,
		costKnown,
	}
	values := [...]float64{s.Demand, s.Pain, s.Trend, s.Competition}

	var sumW, sumWV float64
	for i := 0; i < 4; i++ {
		if !avail[i] {
			continue
		}
		sumW += weights[i]
		sumWV += weights[i] * values[i]
	}
	if costKnown {
		sumW += weights[4]
		sumWV += weights[4] * *s.Cost
	}

	if sumW == 0 {
		s.Overall, s.Confidence = 0, 0
		reasons = append(reasons, "所有维度都缺数据：本次不给结论，请检查数据源或换个品类词")
	} else {
		s.Overall = round2(sumWV / sumW)
		s.Confidence = round2(sumW)
		if !costKnown {
			reasons = append(reasons, fmt.Sprintf(
				"D5 未测算：综合分只覆盖 %.2f 的权重，本次不判「值得验证」", sumW))
		}
	}
	return s, reasons
}

// Verdict 给出结论。costKnown 必须由调用方显式传入——
// "没有成本数据就说值得投产"是这个产品最容易被质疑的地方，不能靠零值蒙混。
func Verdict(s Scores, costKnown bool) string {
	if s.Overall < 45 || s.Competition >= 80 {
		return "不建议"
	}
	if s.Overall >= 70 && s.Pain >= 50 && costKnown {
		return "值得验证"
	}
	return "需进一步验证"
}

// EstimateCost 计算单台成本、建议售价、毛利与 D5 得分。
//
// use_mode=1 的语义是"每 M 台用 1 个"（包装箱类），Qty 存的就是 M。
// 写成 Qty×单价 会让包装成本虚高 M 倍——这是从 RFERP 的 BOM 语义里继承来的坑。
func EstimateCost(lines []CostLine, target *float64, prices []float64) (CostBreakdown, bool) {
	var total float64
	for _, l := range lines {
		if l.UnitPrice < 0 || l.LossRate < 0 {
			continue
		}
		perUnit := l.Qty
		if l.UseMode == 1 {
			if l.Qty <= 0 {
				continue
			}
			perUnit = 1 / l.Qty
		}
		if perUnit <= 0 {
			continue
		}
		total += l.UnitPrice * perUnit * (1 + l.LossRate/100)
	}
	if total <= 0 {
		return CostBreakdown{}, false
	}

	out := CostBreakdown{TotalCost: total}
	switch {
	case target != nil && *target > 0:
		out.Suggested = *target
		out.PriceSource = "手工指定"
	case average(prices) > 0:
		out.Suggested = average(prices) * 0.8
		out.PriceSource = "竞品均价×0.8"
	default:
		out.Suggested = total / 0.6
		out.PriceSource = "成本反推（保证 40% 毛利）"
	}
	if out.Suggested <= 0 {
		return CostBreakdown{}, false
	}
	out.Margin = (out.Suggested - out.TotalCost) / out.Suggested
	if out.Margin >= 0.40 {
		out.Score = 100
	} else {
		out.Score = clamp(out.Margin/0.40*100, 0, 100)
	}
	out.Score = round2(out.Score)
	return out, true
}

func demandScore(items []Evidence) float64 {
	if len(items) == 0 {
		return 0
	}
	hits := float64(len(items))
	return round2(clamp(
		40*math.Log10(1+weightedVotes(items))+
			30*math.Log10(1+weightedComments(items))+
			3*hits, 0, 100))
}

func weightedVotes(items []Evidence) float64 {
	var sum float64
	for _, it := range items {
		sum += float64(it.Votes) * authorityWeight(it.Authority)
	}
	return sum
}

func weightedComments(items []Evidence) float64 {
	var sum float64
	for _, it := range items {
		sum += float64(it.Comments) * authorityWeight(it.Authority)
	}
	return sum
}

func authorityWeight(a float64) float64 {
	return 0.5 + 0.5*clamp(a, 0, 4)/4
}

func painScore(pains []Pain) float64 {
	_, sum := painStats(pains)
	return round2(clamp(4*float64(sum), 0, 100))
}

func painStats(pains []Pain) (count, severitySum int) {
	for _, p := range pains {
		if p.Severity < 1 || p.Severity > 5 {
			continue
		}
		if !validEvidenceURL(p.EvidenceURL) {
			continue
		}
		count++
		severitySum += p.Severity
	}
	return count, severitySum
}

// validEvidenceURL 只做形状校验；真正的"链接是否来自本次检索结果"由 service 层
// 用白名单拦截——那需要持有本次检索的 URL 集合，不适合放在纯函数里。
func validEvidenceURL(raw string) bool {
	u := strings.TrimSpace(raw)
	return strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://")
}

func trendScore(global []Evidence, hotHit bool, now time.Time) float64 {
	if len(global) == 0 && !hotHit {
		return 0
	}
	score := recentRatio(global, now) * 60
	if hotHit {
		score += 40
	}
	return round2(clamp(score, 0, 100))
}

func recentRatio(items []Evidence, now time.Time) float64 {
	if len(items) == 0 {
		return 0
	}
	recent := 0
	for _, it := range items {
		if it.PublishedAt != nil && now.Sub(*it.PublishedAt) <= 30*24*time.Hour {
			recent++
		}
	}
	return float64(recent) / float64(len(items))
}

func competitionScore(global []Evidence, prices []float64) float64 {
	if len(global) == 0 {
		return 0
	}
	return round2(clamp(6*float64(competitorCount(global))+20*math.Min(1, priceSpread(prices)), 0, 100))
}

// competitorCount 用"去重域名数"近似竞争密度。
//
// 不能用搜索结果条数：条数由请求的 Count 决定（要 20 条就返回 20 条），
// 拿它当竞品数会让所有品类都顶格 100——实测确实如此，第一版就这么错了。
// 域名数至少反映"有多少个玩家在卖"，虽然粗糙，但方向是对的。
func competitorCount(items []Evidence) int {
	seen := make(map[string]bool, len(items))
	for _, it := range items {
		host := hostOf(it.URL)
		if host == "" {
			continue
		}
		seen[host] = true
	}
	return len(seen)
}

// hostOf 取 URL 的主机名并去掉 www.；解析失败返回空。
func hostOf(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	return strings.TrimPrefix(host, "www.")
}

func priceSpread(prices []float64) float64 {
	if len(prices) < 2 {
		return 0
	}
	sorted := append([]float64(nil), prices...)
	sort.Float64s(sorted)
	mean := average(sorted)
	if mean <= 0 {
		return 0
	}
	return (sorted[len(sorted)-1] - sorted[0]) / mean
}

func average(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	var sum float64
	for _, x := range xs {
		sum += x
	}
	return sum / float64(len(xs))
}

func hotText(hit bool) string {
	if hit {
		return "热榜命中"
	}
	return "热榜未命中"
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func round2(v float64) float64 {
	return math.Round(v*100) / 100
}
