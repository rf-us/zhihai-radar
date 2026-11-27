package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rf-us/zhihai-radar/internal/config"
	"github.com/rf-us/zhihai-radar/internal/domain"
	"github.com/rf-us/zhihai-radar/internal/reef"
	"github.com/rf-us/zhihai-radar/internal/store"
	"github.com/rf-us/zhihai-radar/internal/supply"
)

func TestBuildAdoptInputMapsSupplierAndBOM(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "radar.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	s := New(config.Config{Source: "fixture"}, st)
	ctx := context.Background()
	o := &store.Opportunity{Keyword: "咖啡机", Name: "便携咖啡机", Scores: domain.Scores{Demand: 80}}
	if err := st.CreateOpportunity(ctx, o); err != nil {
		t.Fatal(err)
	}

	// BOM 两行：外壳、滤网
	bom := []domain.CostLine{
		{PartName: "外壳", Qty: 2, UnitPrice: 40, LossRate: 5},
		{PartName: "滤网", Qty: 1, UnitPrice: 12, UseMode: 0},
	}
	target := 200.0
	cb, _ := domain.EstimateCost(bom, &target, nil)
	if err := st.SaveCost(ctx, o.ID, bom, cb); err != nil {
		t.Fatal(err)
	}

	// 选定供应商：只给"外壳"写供应商
	sup, _ := st.CreateSupplier(ctx, supply.Supplier{Name: "甲供应商"})
	q, _ := st.CreateQuote(ctx, supply.Quote{
		OpportunityID: o.ID, SupplierID: sup.ID, PartName: "外壳",
		UnitPrice: 40, Currency: "CNY", MOQ: 1,
	})
	if err := st.SelectQuote(ctx, o.ID, q.ID); err != nil {
		t.Fatal(err)
	}

	sess := &session{token: "t", user: reef.User{Role: reef.RoleAdmin}}
	in, err := s.buildAdoptInput(ctx, sess, fmt.Sprint(o.ID))
	if err != nil {
		t.Fatal(err)
	}
	if len(in.Parts) != 2 || len(in.Lines) != 2 {
		t.Fatalf("零件/BOM 行数不对：%+v", in)
	}
	if in.Parts[0].Supplier == "" {
		t.Fatal("选定报价的供应商应写入零件")
	}
	if in.Lines[0].PartRef != "外壳" || in.Lines[0].Qty != 2 {
		t.Fatalf("BOM 行映射错误：%+v", in.Lines[0])
	}
}

func TestAdoptRequiresCost(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "radar.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	_ = st.Migrate(context.Background())
	s := New(config.Config{Source: "fixture"}, st)
	o := &store.Opportunity{Keyword: "x", Name: "x"}
	_ = st.CreateOpportunity(context.Background(), o)
	sess := &session{token: "t", user: reef.User{Role: reef.RoleAdmin}, expires: time.Now().Add(time.Hour)}
	const sid = "adopt-session"
	s.sessions[sid] = sess

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/opportunities/"+fmt.Sprint(o.ID)+"/adopt", strings.NewReader(""))
	req.AddCookie(&http.Cookie{Name: cookieName, Value: sid})
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "成本") {
		t.Fatalf("无成本应 400 并提示成本：%d %s", rec.Code, rec.Body.String())
	}
}

func TestAdoptBlockedInReadOnlyMode(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "radar.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	_ = st.Migrate(context.Background())
	cfg := config.Config{Source: "fixture", ReadOnly: true}
	s := New(cfg, st)
	o := &store.Opportunity{Keyword: "x", Name: "x"}
	_ = st.CreateOpportunity(context.Background(), o)
	const sid = "readonly-session"
	s.sessions[sid] = &session{token: "t", user: reef.User{Role: reef.RoleAdmin}, expires: time.Now().Add(time.Hour)}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/opportunities/"+fmt.Sprint(o.ID)+"/adopt", strings.NewReader(""))
	req.AddCookie(&http.Cookie{Name: cookieName, Value: sid})
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "只读") {
		t.Fatalf("只读模式应 403：%d %s", rec.Code, rec.Body.String())
	}
}
