// 命令行分析工具：不经过 HTTP、不需要 RFERP，直接跑一次真实分析。
//
// 用途：
//   - 联调知乎接口（看真实字段与打分）
//   - 演示前的数据核对
//   - CI 里跑一次真实链路（可选，消耗配额）
//
// 用法：
//
//	ZHIHU_ACCESS_SECRET=xxx go run ./cmd/analyze -keyword 便携咖啡机 -price 159
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/rf-us/zhihai-radar/internal/service"
	"github.com/rf-us/zhihai-radar/internal/source"
	"github.com/rf-us/zhihai-radar/internal/store"
)

func main() {
	keyword := flag.String("keyword", "", "品类关键词")
	category := flag.String("category", "", "品类（可选）")
	price := flag.Float64("price", 0, "目标零售价（人民币，可选）")
	fixture := flag.String("fixture", "", "离线快照目录；设置后不调用真实接口")
	flag.Parse()

	if *keyword == "" {
		log.Fatal("缺少 -keyword")
	}

	var src source.Source
	if *fixture != "" {
		f, err := source.LoadFixtureDir(*fixture)
		if err != nil {
			log.Fatalf("加载快照: %v", err)
		}
		src = f
	} else {
		secret := os.Getenv("ZHIHU_ACCESS_SECRET")
		if secret == "" {
			log.Fatal("未设置 ZHIHU_ACCESS_SECRET（或用 -fixture 跑离线）")
		}
		src = source.NewZhihu(os.Getenv("ZHIHU_BASE"), secret)
	}

	dir, err := os.MkdirTemp("", "radar-analyze-")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(dir)

	st, err := store.Open(filepath.Join(dir, "radar.db"))
	if err != nil {
		log.Fatal(err)
	}
	defer st.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if err := st.Migrate(ctx); err != nil {
		log.Fatal(err)
	}

	var target *float64
	if *price > 0 {
		target = price
	}
	analyzer := service.NewAnalyzer(src, st)
	start := time.Now()
	detail, err := analyzer.Analyze(ctx, service.AnalyzeInput{
		Keyword: *keyword, Category: *category, TargetPrice: target,
	})
	if err != nil {
		log.Fatalf("分析失败: %v", err)
	}

	o := detail.Opportunity
	s := o.Scores
	cost := "未测算"
	if s.Cost != nil {
		cost = fmt.Sprintf("%.0f", *s.Cost)
	}
	fmt.Printf("\n%s（%s）  耗时 %s\n", o.Keyword, o.Category, time.Since(start).Round(time.Millisecond))
	fmt.Printf("结论：%s\n", o.Verdict)
	fmt.Printf("需求 %.0f  痛点 %.0f  趋势 %.0f  竞争 %.0f  成本 %s  综合 %.1f  置信 %.2f\n",
		s.Demand, s.Pain, s.Trend, s.Competition, cost, s.Overall, s.Confidence)
	if detail.Degraded {
		fmt.Printf("⚠️ %s\n", detail.DegradedReason)
	}
	fmt.Println("\n理由：")
	for _, r := range detail.Reasons {
		fmt.Println("  -", r)
	}
	fmt.Printf("\n证据 %d 条（前 5）：\n", len(detail.Evidence))
	for i, e := range detail.Evidence {
		if i >= 5 {
			break
		}
		fmt.Printf("  [%s] %s\n        %s\n", e.Source, truncate(e.Title, 46), truncate(e.URL, 78))
	}
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
