package domain

// RecomputeWithCost 在已有四维分基础上，替换 D5 后重算综合分、置信度与结论。
//
// 为什么不用 Score 全量重算：机会落库时只保存了分数与理由，没有保存知乎/全网
// 原始证据的完整 Input。全量重算要重新抓数据，既费配额又会让同一份材料的分数
// 随接口波动。这里用已落库的四个维度分 + 新 D5，按同一套权重归一化，得到与
// Score 在相同输入下一致的 overall/confidence。
//
// costScore == nil 表示删除成本：D5 不参与，confidence 回落到 0.8，
// 结论退回"需进一步验证"——可重算，不是一次性开关。
func RecomputeWithCost(s Scores, costScore *float64) (Scores, string) {
	out := s
	out.Cost = costScore

	values := [4]float64{s.Demand, s.Pain, s.Trend, s.Competition}
	var sumW, sumWV float64
	for i, w := range weights[:4] {
		sumW += w
		sumWV += w * values[i]
	}
	if costScore != nil {
		sumW += weights[4]
		sumWV += weights[4] * *costScore
	}
	if sumW == 0 {
		out.Overall, out.Confidence = 0, 0
	} else {
		out.Overall = round2(sumWV / sumW)
		out.Confidence = round2(sumW)
	}
	// 成本未测算时绝不判"值得验证"，与 Verdict 的门禁一致。
	return out, Verdict(out, costScore != nil)
}
