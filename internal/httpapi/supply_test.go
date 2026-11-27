package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
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
)

func newSupplyTestServer(t *testing.T) (*Server, int64) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "radar.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeTestStore(t, st) })
	if err := st.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	s := New(config.Config{Source: "fixture", ReefBase: "http://127.0.0.1:1"}, st)
	const sid = "supply-session"
	s.sessions[sid] = &session{token: "t", user: reef.User{Role: reef.RoleAdmin}, expires: time.Now().Add(time.Hour)}

	o := &store.Opportunity{Keyword: "咖啡机", Name: "咖啡机", Scores: domain.Scores{Demand: 80}}
	if err := st.CreateOpportunity(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	return s, o.ID
}

func (s *Server) doSupply(t *testing.T, method, url, body string) *httptest.ResponseRecorder {
	t.Helper()
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, url, nil)
	} else {
		req = httptest.NewRequest(method, url, bytes.NewBufferString(body))
	}
	req.AddCookie(&http.Cookie{Name: cookieName, Value: "supply-session"})
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

func TestSupplyEndpoints(t *testing.T) {
	s, oid := newSupplyTestServer(t)

	// 建两个供应商
	for _, name := range []string{"甲", "乙"} {
		body, _ := json.Marshal(map[string]string{"name": name, "region": "深圳"})
		rec := s.doSupply(t, http.MethodPost, "/api/suppliers", string(body))
		if rec.Code != http.StatusOK {
			t.Fatalf("create supplier %s: %d %s", name, rec.Code, rec.Body.String())
		}
	}

	// 列供应商
	rec := s.doSupply(t, http.MethodGet, "/api/suppliers", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list suppliers: %d %s", rec.Code, rec.Body.String())
	}
	var suppliers []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &suppliers); err != nil || len(suppliers) != 2 {
		t.Fatalf("suppliers: %v %s", err, rec.Body.String())
	}
	sid1 := int64(suppliers[0]["id"].(float64))

	// 建报价（带阶梯价）
	quoteBody := map[string]any{
		"supplier_id": sid1, "part_name": "外壳", "unit_price": 12.5,
		"currency": "CNY", "moq": 100, "lead_time_days": 20,
		"tiers": []map[string]any{{"min_qty": 1000, "unit_price": 10}},
	}
	b, _ := json.Marshal(quoteBody)
	rec = s.doSupply(t, http.MethodPost, "/api/opportunities/"+itoa(oid)+"/quotes", string(b))
	if rec.Code != http.StatusOK {
		t.Fatalf("create quote: %d %s", rec.Code, rec.Body.String())
	}

	// 列报价
	rec = s.doSupply(t, http.MethodGet, "/api/opportunities/"+itoa(oid)+"/quotes", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list quotes: %d %s", rec.Code, rec.Body.String())
	}

	// 比价
	rec = s.doSupply(t, http.MethodGet, "/api/opportunities/"+itoa(oid)+"/quotes/compare?qty=1000", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("compare: %d %s", rec.Code, rec.Body.String())
	}
	var cmp []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &cmp); err != nil || len(cmp) != 1 {
		t.Fatalf("compare result: %v %s", err, rec.Body.String())
	}
	if cmp[0]["recommended"] != true {
		t.Fatalf("单条报价应为推荐：%+v", cmp[0])
	}

	// 选定
	qid := int64(cmp[0]["quote_id"].(float64))
	rec = s.doSupply(t, http.MethodPost, "/api/opportunities/"+itoa(oid)+"/quotes/"+itoa(qid)+"/select", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("select: %d %s", rec.Code, rec.Body.String())
	}
}

func TestSupplyRequiresSession(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "radar.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestStore(t, st)
	s := New(config.Config{Source: "fixture"}, st)
	req := httptest.NewRequest(http.MethodGet, "/api/suppliers", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("未登录应 401，got %d", rec.Code)
	}
}

func itoa(n int64) string {
	return fmt.Sprintf("%d", n)
}

func TestDeleteQuotedSupplierReturns409(t *testing.T) {
	s, oid := newSupplyTestServer(t)
	// 建供应商 + 引用它的报价
	body, _ := json.Marshal(map[string]string{"name": "被引用供应商"})
	rec := s.doSupply(t, http.MethodPost, "/api/suppliers", string(body))
	var sup map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &sup)
	sid := int64(sup["id"].(float64))

	qb, _ := json.Marshal(map[string]any{
		"supplier_id": sid, "part_name": "x", "unit_price": 10, "currency": "CNY", "moq": 1,
	})
	rec = s.doSupply(t, http.MethodPost, "/api/opportunities/"+itoa(oid)+"/quotes", string(qb))
	if rec.Code != http.StatusOK {
		t.Fatalf("建报价失败：%d %s", rec.Code, rec.Body.String())
	}

	rec = s.doSupply(t, http.MethodDelete, "/api/suppliers?id="+itoa(sid), "")
	if rec.Code != http.StatusConflict {
		t.Fatalf("应 409，got %d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), itoa(oid)) {
		t.Fatalf("409 应列出引用它的机会 ID：%s", rec.Body.String())
	}
}
