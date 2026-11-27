package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
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

func newCostTestServer(t *testing.T) (*Server, int64) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "radar.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	s := New(config.Config{Source: "fixture"}, st)
	const sid = "cost-session"
	s.sessions[sid] = &session{token: "t", user: reef.User{Role: reef.RoleAdmin}, expires: time.Now().Add(time.Hour)}
	o := &store.Opportunity{Keyword: "咖啡机", Name: "咖啡机", Scores: domain.Scores{
		Demand: 90, Pain: 80, Trend: 70, Competition: 20, Confidence: 0.8,
	}, Verdict: "需进一步验证"}
	if err := st.CreateOpportunity(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	return s, o.ID
}

func (s *Server) doCost(t *testing.T, method, url, body string) *httptest.ResponseRecorder {
	t.Helper()
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, url, nil)
	} else {
		req = httptest.NewRequest(method, url, strings.NewReader(body))
	}
	req.AddCookie(&http.Cookie{Name: cookieName, Value: "cost-session"})
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

func TestCostSaveRecomputeAndDelete(t *testing.T) {
	s, oid := newCostTestServer(t)
	url := "/api/opportunities/" + fmt.Sprint(oid) + "/cost"

	body := `{"bom":[{"part_name":"外壳","qty":2,"unit_price":40,"loss_rate":5}],"target_price":200}`
	rec := s.doCost(t, http.MethodPost, url, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("save cost: %d %s", rec.Code, rec.Body.String())
	}

	// 机会分数应更新到 1.0。
	o, err := s.st.GetOpportunity(context.Background(), oid)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(o.Scores.Confidence-1.0) > 1e-9 || o.Verdict != "值得验证" {
		t.Fatalf("填成本后应 1.0/值得验证：%+v %s", o.Scores, o.Verdict)
	}

	rec = s.doCost(t, http.MethodGet, url, "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "total_cost") {
		t.Fatalf("get cost: %d %s", rec.Code, rec.Body.String())
	}

	rec = s.doCost(t, http.MethodDelete, url, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("delete cost: %d %s", rec.Code, rec.Body.String())
	}
	o, _ = s.st.GetOpportunity(context.Background(), oid)
	if math.Abs(o.Scores.Confidence-0.8) > 1e-9 || o.Verdict != "需进一步验证" {
		t.Fatalf("删成本后应回 0.8：%+v %s", o.Scores, o.Verdict)
	}
}

func TestCostUseModeDivide(t *testing.T) {
	s, oid := newCostTestServer(t)
	url := "/api/opportunities/" + fmt.Sprint(oid) + "/cost"
	body := `{"bom":[{"part_name":"包装箱","qty":1000,"unit_price":300,"use_mode":1}],"target_price":10}`
	rec := s.doCost(t, http.MethodPost, url, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("save: %d %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Cost domain.CostBreakdown `json:"cost"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if math.Abs(out.Cost.TotalCost-0.3) > 1e-9 {
		t.Fatalf("use_mode=1000 单台成本应为 0.3，got %v", out.Cost.TotalCost)
	}
}

func TestCostRejectsInvalid(t *testing.T) {
	s, oid := newCostTestServer(t)
	url := "/api/opportunities/" + fmt.Sprint(oid) + "/cost"
	for _, body := range []string{
		`{"bom":[]}`,
		`{"bom":[{"unit_price":-1}]}`,
		`{"bom":[{"unit_price":10,"loss_rate":-1}]}`,
		`{"bom":[{"unit_price":10}],"target_price":0}`,
	} {
		rec := s.doCost(t, http.MethodPost, url, body)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("应 400：%s got %d %s", body, rec.Code, rec.Body.String())
		}
	}
}
