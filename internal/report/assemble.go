package report

import (
	"fmt"
	"strings"
	"time"

	"github.com/rf-us/zhihai-radar/internal/domain"
	"github.com/rf-us/zhihai-radar/internal/store"
	"github.com/rf-us/zhihai-radar/internal/supply"
)

// AssembleInput 是报告组装所需的全部已落库数据。
type AssembleInput struct {
	Opportunity store.Opportunity
	Evidence    []store.Evidence
	Cost        *store.CostRecord
	Quotes      []supply.Quote
	Comparison  []supply.Comparison
	// Sections 是条件章节 key（compliance / logistics / aftersales），来自问答顾虑。
	Sections []string
	// Now 由调用方注入，保证可复现；为零时用机会的创建时间。
	Now time.Time
}

// Assemble 把已落库数据组装成报告文档。
//
// 规则与 docs/report-framework.md 一致：
//   - 先结论后数据：每节 Lead 是判断，Table/证据是支撑；
//   - 无数据不编：缺失维度写"未测算"，不用 0 冒充；
//   - 证据可回溯：每条带 URL 与采集时间；
//   - 条件章节只由 Sections 触发。
func Assemble(in AssembleInput) Document {
	opp := in.Opportunity
	now := in.Now
	if now.IsZero() {
		now = opp.CreatedAt
	}
	if now.IsZero() {
		now = time.Now()
	}

	doc := Document{
		Title: opp.Name + " 出海选品调研报告",
		Meta: Meta{
			GeneratedAt: now.UTC().Format(time.RFC3339),
			Keyword:     opp.Keyword,
			Market:      opp.Category,
			Source:      "知乎开放平台 + 离线快照",
			SnapshotAt:  now.UTC().Format("2006-01-02"),
		},
	}

	// 1 结论摘要
	verdict := opp.Verdict
	if verdict == "" {
		verdict = "未出结论"
	}
	lead := fmt.Sprintf("综合分 %.1f（覆盖权重 %.0f%%），结论：%s。",
		opp.Scores.Overall, opp.Scores.Confidence*100, verdict)
	if len(opp.Reasons) > 0 {
		lead += " " + opp.Reasons[0]
	}
	summary := Section{Key: "summary", Heading: "一、结论摘要", Lead: lead}
	for _, r := range opp.Reasons {
		summary.Bullets = append(summary.Bullets, r)
	}
	doc.Sections = append(doc.Sections, summary)

	// 2 需求与痛点（D1/D2 + 证据）
	demand := Section{
		Key: "demand", Heading: "二、需求与痛点",
		Lead: fmt.Sprintf("D1 需求热度 %.0f，D2 痛点强度 %.0f；以下证据均可点开原文。", opp.Scores.Demand, opp.Scores.Pain),
	}
	for _, ev := range in.Evidence {
		if ev.Source != "zhihu_search" && ev.Source != "hot" {
			continue
		}
		demand.Evidence = append(demand.Evidence, EvidenceRef{
			Title: ev.Title, URL: ev.URL, Summary: ev.Snippet,
			CollectedAt: collectedAt(ev),
		})
		if len(demand.Evidence) >= 8 {
			break
		}
	}
	if len(demand.Evidence) == 0 {
		demand.Lead += " 本次未取到知乎证据。"
	}
	doc.Sections = append(doc.Sections, demand)

	// 3 竞争格局（D4 + 竞品表）
	comp := Section{Key: "competition", Heading: "三、竞争格局",
		Lead: fmt.Sprintf("D4 竞争供给 %.0f（越高越不推荐）。", opp.Scores.Competition)}
	if len(in.Comparison) > 0 {
		tbl := &Table{Headers: []string{"供应商", "单价", "总价", "交期", "备注"}}
		for _, c := range in.Comparison {
			tbl.Rows = append(tbl.Rows, []string{
				c.SupplierName, fmt.Sprintf("%.2f", c.UnitPrice),
				fmt.Sprintf("%.2f %s", c.Total, c.Currency),
				fmt.Sprintf("%d 天", c.LeadTimeDays), c.Reason,
			})
		}
		comp.Table = tbl
	} else {
		comp.Lead += " 供应链数据未测算。"
	}
	doc.Sections = append(doc.Sections, comp)

	// 4 成本与定价（D5 + BOM 表）
	cost := Section{Key: "cost", Heading: "四、成本与定价"}
	if in.Cost == nil {
		cost.Lead = "D5 未测算：本次不给成本结论。补齐成本后可重新生成报告。"
	} else {
		cb := in.Cost.Breakdown
		cost.Lead = fmt.Sprintf("D5 %.0f：单台成本 ¥%.2f，建议售价 ¥%.2f（%s），毛利 %.0f%%。",
			cb.Score, cb.TotalCost, cb.Suggested, cb.PriceSource, cb.Margin*100)
		tbl := &Table{Headers: []string{"零件", "数量", "单价", "损耗率", "计费方式"}}
		for _, line := range in.Cost.BOM {
			mode := "每台"
			if line.UseMode == 1 {
				mode = fmt.Sprintf("每 %.0f 台 1 个", line.Qty)
			}
			tbl.Rows = append(tbl.Rows, []string{
				line.PartName, fmt.Sprintf("%.2f", line.Qty),
				fmt.Sprintf("%.2f", line.UnitPrice),
				fmt.Sprintf("%.0f%%", line.LossRate), mode,
			})
		}
		cost.Table = tbl
	}
	doc.Sections = append(doc.Sections, cost)

	// 5 供应链方案（多供应商对比表）
	supplySec := Section{Key: "supply", Heading: "五、供应链方案"}
	if len(in.Quotes) == 0 {
		supplySec.Lead = "暂无供应商报价。"
	} else {
		selected := ""
		for _, q := range in.Quotes {
			if q.Selected {
				selected = q.SupplierName
			}
		}
		if selected != "" {
			supplySec.Lead = fmt.Sprintf("已选定：%s。以下为全部报价对比（示例数据以现场说明为准）。", selected)
		} else {
			supplySec.Lead = "尚未选定供应商。以下为报价对比。"
		}
		tbl := &Table{Headers: []string{"供应商", "零件", "单价", "MOQ", "交期"}}
		for _, q := range in.Quotes {
			tbl.Rows = append(tbl.Rows, []string{
				q.SupplierName, q.PartName, fmt.Sprintf("%.2f %s", q.UnitPrice, q.Currency),
				fmt.Sprintf("%d", q.MOQ), fmt.Sprintf("%d 天", q.LeadTimeDays),
			})
		}
		supplySec.Table = tbl
	}
	doc.Sections = append(doc.Sections, supplySec)

	// 6 风险与下一步
	risk := Section{Key: "risk", Heading: "六、风险与下一步",
		Lead: "以下假设尚未验证，需要线下核实。"}
	for _, r := range opp.Reasons {
		if strings.Contains(r, "未测算") || strings.Contains(r, "未取到") {
			risk.Bullets = append(risk.Bullets, r)
		}
	}
	if len(risk.Bullets) == 0 {
		risk.Bullets = []string{"核实目标市场认证要求", "确认供应商 MOQ 与交期", "实测长期使用的可靠性"}
	}
	doc.Sections = append(doc.Sections, risk)

	// 条件章节：只由问答顾虑触发。
	cond := map[string]Section{
		"compliance": {Key: "compliance", Heading: "附 A、合规与认证",
			Lead:    "目标市场认证要求需按品类与当地法规核实；本报告不替代合规意见。",
			Bullets: []string{"核对食品接触/电气安全等适用标准", "确认标签与说明书语言要求"}},
		"logistics": {Key: "logistics", Heading: "附 B、物流与包装",
			Lead:    "体积重与头程成本显著影响毛利，需按实际渠道复核。",
			Bullets: []string{"测量包装后体积重", "对比海运/空运头程报价"}},
		"aftersales": {Key: "aftersales", Heading: "附 C、售后与退换",
			Lead:    "备件策略与退换率决定长期成本。",
			Bullets: []string{"梳理易损件清单", "估算退货率对毛利的影响"}},
	}
	for _, key := range in.Sections {
		if sec, ok := cond[key]; ok {
			sec.Conditional = true
			doc.Sections = append(doc.Sections, sec)
		}
	}
	return doc
}

func collectedAt(ev store.Evidence) string {
	if ev.PublishedAt == nil {
		return "时间未标注"
	}
	return ev.PublishedAt.UTC().Format("2006-01-02")
}

// 确保 domain 被引用（CostLine 通过 CostRecord 间接使用，这里显式占位避免误删）。
var _ = domain.CostLine{}
