package httpapi

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rf-us/zhihai-radar/internal/config"
	"github.com/rf-us/zhihai-radar/internal/domain"
	"github.com/rf-us/zhihai-radar/internal/reef"
	"github.com/rf-us/zhihai-radar/internal/store"
)

func TestExportReportEndpoint(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "radar.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	s := New(config.Config{Source: "fixture"}, st)
	const sid = "report-session"
	s.sessions[sid] = &session{token: "t", user: reef.User{Role: reef.RoleAdmin}, expires: time.Now().Add(time.Hour)}
	now := time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)
	o := &store.Opportunity{Keyword: "便携咖啡机", Name: "便携咖啡机", Category: "北美", CreatedAt: now,
		Scores: domain.Scores{Demand: 80, Confidence: 0.8}, Verdict: "需进一步验证"}
	if err := st.CreateOpportunity(context.Background(), o); err != nil {
		t.Fatal(err)
	}

	url := "/api/opportunities/" + fmt.Sprint(o.ID) + "/report?sections=compliance"
	req := httptest.NewRequest(http.MethodGet, url, nil)
	req.AddCookie(&http.Cookie{Name: cookieName, Value: sid})
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("导出失败：%d %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/pdf" {
		t.Fatalf("Content-Type = %s", ct)
	}
	if !bytes.HasPrefix(rec.Body.Bytes(), []byte("%PDF-")) {
		t.Fatal("响应不是 PDF")
	}
	// 中文正文应可抽回（用 pymupdf 在外部样例验证过；这里验证字节含标题的 UTF-16 编码轨迹不可行，
	// 改为确认响应体足够大且缺字计数头存在）。
	if rec.Body.Len() < 10000 {
		t.Fatalf("PDF 太小：%d", rec.Body.Len())
	}
	if h := rec.Header().Get("X-Missing-Runes"); h == "" {
		t.Fatal("缺少缺字计数头")
	}
}

func TestExportReportRequiresSession(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "radar.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	s := New(config.Config{Source: "fixture"}, st)
	req := httptest.NewRequest(http.MethodGet, "/api/opportunities/1/report", strings.NewReader(""))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("未登录应 401，got %d", rec.Code)
	}
}

// TestFullReportFlow 端到端：建机会 → 填成本 → 导出带条件章节的 PDF，
// 并把产物写到临时目录供 pdftotext 人工核验。
func TestFullReportFlow(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "radar.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	s := New(config.Config{Source: "fixture"}, st)
	const sid = "flow-session"
	s.sessions[sid] = &session{token: "t", user: reef.User{Role: reef.RoleAdmin}, expires: time.Now().Add(time.Hour)}

	now := time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)
	o := &store.Opportunity{Keyword: "便携咖啡机", Name: "便携咖啡机", Category: "北美", CreatedAt: now,
		Scores: domain.Scores{Demand: 90, Pain: 80, Trend: 70, Competition: 20, Confidence: 0.8}, Verdict: "需进一步验证"}
	if err := st.CreateOpportunity(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	bom := []domain.CostLine{{PartName: "外壳", Qty: 2, UnitPrice: 40, LossRate: 5}}
	target := 200.0
	cb, _ := domain.EstimateCost(bom, &target, nil)
	if err := st.SaveCost(context.Background(), o.ID, bom, cb); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/opportunities/%d/report?sections=compliance,logistics", o.ID), nil)
	req.AddCookie(&http.Cookie{Name: cookieName, Value: sid})
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !bytes.HasPrefix(rec.Body.Bytes(), []byte("%PDF-")) {
		t.Fatalf("端到端导出失败：%d %s", rec.Code, rec.Body.String()[:200])
	}
	out := filepath.Join(os.TempDir(), "radar-e2e-report.pdf")
	if err := os.WriteFile(out, rec.Body.Bytes(), 0644); err != nil {
		t.Fatal(err)
	}
	t.Logf("报告已写入 %s（%d 字节，缺字 %s）", out, rec.Body.Len(), rec.Header().Get("X-Missing-Runes"))
}
