package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/rf-us/zhihai-radar/internal/service"
	"github.com/rf-us/zhihai-radar/internal/store"
)

func (s *Server) handleAnalyze(w http.ResponseWriter, r *http.Request, sess *session) {
	var body struct {
		Keyword     string   `json:"keyword"`
		Category    string   `json:"category"`
		TargetPrice *float64 `json:"target_price"`
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if body.TargetPrice != nil && *body.TargetPrice <= 0 {
		writeErr(w, http.StatusBadRequest, "target_price 必须大于 0")
		return
	}

	detail, err := s.analyzer.Analyze(r.Context(), service.AnalyzeInput{
		Keyword:     body.Keyword,
		Category:    body.Category,
		TargetPrice: body.TargetPrice,
		Operator:    operatorOf(sess),
	})
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "not found")
			return
		}
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

func (s *Server) handleListOpportunities(w http.ResponseWriter, r *http.Request, _ *session) {
	limit := 30
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 || n > 200 {
			writeErr(w, http.StatusBadRequest, "invalid limit")
			return
		}
		limit = n
	}
	list, err := s.analyzer.List(r.Context(), limit)
	if err != nil {
		writeServerErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleGetOpportunity(w http.ResponseWriter, r *http.Request, _ *session) {
	id, err := strconv.ParseInt(r.URL.Query().Get("id"), 10, 64)
	if err != nil || id <= 0 {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	detail, err := s.analyzer.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "not found")
			return
		}
		writeServerErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

func operatorOf(sess *session) string {
	if sess == nil {
		return ""
	}
	if sess.user.DisplayName != "" {
		return sess.user.DisplayName
	}
	return sess.user.Username
}
