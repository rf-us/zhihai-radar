package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rf-us/zhihai-radar/internal/config"
	"github.com/rf-us/zhihai-radar/internal/service"
	"github.com/rf-us/zhihai-radar/internal/source"
	"github.com/rf-us/zhihai-radar/internal/store"
)

const dialogueURL = "/api/dialogue/sessions"
const coffeeMessage = "想卖便携咖啡机到北美，定价 $50，长期代工，关心认证和售后，想看完整证据链"

func newDialogueTestServer(t *testing.T, src source.Source) (*Server, http.Handler) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "radar.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeTestStore(t, st) })
	if err := st.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	s := New(config.Config{Source: "fixture"}, st)
	s.analyzer = service.NewAnalyzer(src, st)
	for _, id := range []string{"first", "second"} {
		s.sessions[id] = &session{expires: time.Now().Add(time.Hour)}
	}
	return s, s.Handler()
}

func callDialogue(t *testing.T, handler http.Handler, owner, method, path string, body any, want int) dialogueView {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(method, path, strings.NewReader(string(data)))
	req.AddCookie(&http.Cookie{Name: cookieName, Value: owner})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != want {
		t.Fatalf("%s %s: got %d want %d: %s", method, path, rec.Code, want, rec.Body.String())
	}
	var view dialogueView
	if want < 300 && want != 204 {
		if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
			t.Fatal(err)
		}
	}
	return view
}

func waitDialogue(t *testing.T, handler http.Handler, path string, ready func(dialogueView) bool) dialogueView {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		view := callDialogue(t, handler, "first", http.MethodGet, path, nil, http.StatusOK)
		if ready(view) {
			return view
		}
		if time.Now().After(deadline) {
			t.Fatalf("did not see expected dialogue state: %+v", view.Analysis)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestDialogueHTTPRoundTripAndOwnership(t *testing.T) {
	fixture, err := source.LoadFixtureDir("../../fixtures")
	if err != nil {
		t.Fatal(err)
	}
	s, handler := newDialogueTestServer(t, fixture)
	view := callDialogue(t, handler, "first", http.MethodPost, dialogueURL, nil, http.StatusOK)
	path := dialogueURL + "/" + view.ID
	callDialogue(t, handler, "second", http.MethodGet, path, nil, http.StatusNotFound)
	callDialogue(t, handler, "second", http.MethodPost, path+"/analyze", dialogueRevision{view.Revision}, http.StatusNotFound)
	for index, text := range []string{"想看便携咖啡机", "北美", "$50", "长期代工", "今天 28 号天气怎么样"} {
		view = callDialogue(t, handler, "first", http.MethodPost, path+"/turns",
			map[string]any{"revision": view.Revision, "text": text}, http.StatusOK)
		want := "collecting"
		if index >= 3 {
			want = "ready"
		}
		if view.Status != want {
			t.Fatalf("turn %d: %+v", index+1, view)
		}
	}
	if view.Messages[9].Kind != "refused" {
		t.Fatal(view.Messages[9])
	}

	for slot, value := range map[string]string{
		"direction": "便携咖啡机", "market": "eu", "price": "usd_80_150",
		"supply": "own", "concerns": "certification,aftersales", "depth": "full",
	} {
		view = callDialogue(t, handler, "first", http.MethodPut, path+"/slots/"+slot,
			map[string]any{"revision": view.Revision, "value": value}, http.StatusOK)
		if view.Slots[slot] != value {
			t.Fatalf("%s=%q", slot, view.Slots[slot])
		}
	}
	callDialogue(t, handler, "first", http.MethodPut, path+"/slots/market",
		map[string]any{"revision": view.Revision, "value": "unknown"}, http.StatusBadRequest)
	resumed := callDialogue(t, handler, "first", http.MethodGet, path, nil, http.StatusOK)
	if resumed.Revision != view.Revision || resumed.Slots["market"] != "eu" || len(resumed.Messages) != 16 {
		t.Fatalf("unexpected restoration: %+v", resumed)
	}
	var starts sync.WaitGroup
	statuses := make(chan int, 2)
	for range 2 {
		starts.Go(func() {
			req := httptest.NewRequest(http.MethodPost, path+"/analyze",
				strings.NewReader(fmt.Sprintf(`{"revision":%d}`, view.Revision)))
			req.AddCookie(&http.Cookie{Name: cookieName, Value: "first"})
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, req)
			statuses <- response.Code
		})
	}
	starts.Wait()
	first, second := <-statuses, <-statuses
	if min(first, second) != http.StatusAccepted || max(first, second) != http.StatusConflict {
		t.Fatalf("concurrent start: got HTTP %d and %d", first, second)
	}
	view = waitDialogue(t, handler, path, func(view dialogueView) bool { return view.Status == "done" })
	detail, err := s.analyzer.Get(context.Background(), view.OpportunityID)
	if err != nil {
		t.Fatal(err)
	}
	analysis := detail.Opportunity.Analysis
	if detail.Opportunity.Keyword != "便携咖啡机" || detail.Opportunity.Category != "家居小家电" ||
		analysis.Plan.Market != "eu" || analysis.Plan.Depth != "full" || len(analysis.Plan.Sections) != 2 ||
		analysis.Mode != "fixture" || len(analysis.Sources) != 4 || len(detail.Evidence) == 0 {
		t.Fatalf("unexpected saved analysis: %+v / %+v", detail, analysis)
	}
	for name, result := range analysis.Sources {
		if result.Status != "succeeded" {
			t.Fatalf("%s: %+v", name, result)
		}
	}
}

type interruptedSource struct {
	source.Unavailable
	hotStarted chan struct{}
	hotExited  chan struct{}
	failing    bool
}

func (src interruptedSource) SearchZhihu(context.Context, string, int) ([]source.Post, error) {
	return nil, source.ErrQuotaExhausted
}

func (src interruptedSource) SearchGlobal(context.Context, string, int) ([]source.Post, error) {
	return nil, source.ErrRateLimited
}

func (src interruptedSource) Hot(ctx context.Context, _ int) ([]source.Post, error) {
	if src.failing {
		return nil, nil
	}
	close(src.hotStarted)
	<-ctx.Done()
	close(src.hotExited)
	return nil, ctx.Err()
}

func TestDialogueCancelFailureAndRetry(t *testing.T) {
	src := interruptedSource{hotStarted: make(chan struct{}), hotExited: make(chan struct{})}
	s, handler := newDialogueTestServer(t, src)
	s.limiter.sessionWindow = 0
	view := callDialogue(t, handler, "first", http.MethodPost, dialogueURL, nil, http.StatusOK)
	path := dialogueURL + "/" + view.ID
	view = callDialogue(t, handler, "first", http.MethodPost, path+"/turns",
		map[string]any{"revision": view.Revision, "text": coffeeMessage}, http.StatusOK)
	if view.Status != "ready" || len(view.Messages[1].Changed) != 6 {
		t.Fatal(view)
	}
	view = callDialogue(t, handler, "first", http.MethodPost, path+"/analyze", dialogueRevision{view.Revision}, http.StatusAccepted)
	select {
	case <-src.hotStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("analysis never called Hot")
	}
	waitDialogue(t, handler, path, func(view dialogueView) bool {
		return view.Analysis.Sources[service.Hot].Status == "running" &&
			view.Analysis.Sources[service.GlobalSearch].Reason == "上游限流"
	})
	view = callDialogue(t, handler, "first", http.MethodPost, path+"/cancel", dialogueRevision{view.Revision}, http.StatusOK)
	if view.Status != "ready" || view.Analysis.Status != "canceled" || view.Analysis.Sources[service.Hot].Status != "canceled" {
		t.Fatal(view)
	}
	select {
	case <-src.hotExited:
	case <-time.After(3 * time.Second):
		t.Fatal("source did not receive cancellation")
	}
	s.analyzer = service.NewAnalyzer(interruptedSource{failing: true}, s.st)
	callDialogue(t, handler, "first", http.MethodPost, path+"/analyze", dialogueRevision{view.Revision}, http.StatusAccepted)
	view = waitDialogue(t, handler, path, func(view dialogueView) bool { return view.Analysis.Status == "failed" })
	if view.Status != "ready" || view.OpportunityID != 0 || view.Analysis.Sources[service.ZhihuSearch].Reason != "配额耗尽" {
		t.Fatal(view)
	}
	fixture, err := source.LoadFixtureDir("../../fixtures")
	if err != nil {
		t.Fatal(err)
	}
	s.analyzer = service.NewAnalyzer(fixture, s.st)
	if _, err := s.st.DB().Exec(`CREATE TRIGGER reject_evidence BEFORE INSERT ON opportunity_evidence
		BEGIN SELECT RAISE(ABORT, 'injected evidence write failure'); END`); err != nil {
		t.Fatal(err)
	}
	callDialogue(t, handler, "first", http.MethodPost, path+"/analyze", dialogueRevision{view.Revision}, http.StatusAccepted)
	view = waitDialogue(t, handler, path, func(view dialogueView) bool { return view.Analysis.Status == "failed" })
	if view.Analysis.Error != "保存分析结果失败，请重试" || view.OpportunityID != 0 {
		t.Fatal(view.Analysis)
	}
	if rows, err := s.st.ListOpportunities(context.Background(), 10); err != nil || len(rows) != 0 {
		t.Fatalf("partial opportunity after rollback: %+v %v", rows, err)
	}
	if _, err := s.st.DB().Exec(`DROP TRIGGER reject_evidence`); err != nil {
		t.Fatal(err)
	}
	callDialogue(t, handler, "first", http.MethodPost, path+"/analyze", dialogueRevision{view.Revision}, http.StatusAccepted)
	view = waitDialogue(t, handler, path, func(view dialogueView) bool { return view.Status == "done" })
	opportunities, err := s.st.ListOpportunities(context.Background(), 10)
	if err != nil || len(opportunities) != 1 || opportunities[0].ID != view.OpportunityID {
		t.Fatalf("expected exactly one committed opportunity: %+v %v", opportunities, err)
	}
	callDialogue(t, handler, "first", http.MethodPost, path+"/cancel", dialogueRevision{view.Revision - 1}, http.StatusOK)
	callDialogue(t, handler, "first", http.MethodPost, path+"/analyze", dialogueRevision{view.Revision}, http.StatusConflict)
	path = fmt.Sprintf("%s/%s/next", dialogueURL, view.ID)
	next := callDialogue(t, handler, "first", http.MethodPost, path, dialogueRevision{view.Revision}, http.StatusCreated)
	if next.ID == view.ID || next.Status != "collecting" {
		t.Fatal(next)
	}
}
