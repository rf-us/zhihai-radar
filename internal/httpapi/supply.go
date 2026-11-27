package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/rf-us/zhihai-radar/internal/store"
	"github.com/rf-us/zhihai-radar/internal/supply"
)

// handleListSuppliers GET /api/suppliers
func (s *Server) handleListSuppliers(w http.ResponseWriter, r *http.Request, _ *session) {
	list, err := s.st.ListSuppliers(r.Context())
	if err != nil {
		writeServerErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// handleCreateSupplier POST /api/suppliers
func (s *Server) handleCreateSupplier(w http.ResponseWriter, r *http.Request, _ *session) {
	var body supply.Supplier
	if err := decodeJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	created, err := s.st.CreateSupplier(r.Context(), body)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, created)
}

// handleListQuotes GET /api/opportunities/{id}/quotes
func (s *Server) handleListQuotes(w http.ResponseWriter, r *http.Request, _ *session) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	list, err := s.st.ListQuotes(r.Context(), id)
	if err != nil {
		writeServerErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// handleCreateQuote POST /api/opportunities/{id}/quotes
func (s *Server) handleCreateQuote(w http.ResponseWriter, r *http.Request, _ *session) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	var body struct {
		SupplierID   int64         `json:"supplier_id"`
		PartName     string        `json:"part_name"`
		UnitPrice    float64       `json:"unit_price"`
		Currency     string        `json:"currency"`
		MOQ          int           `json:"moq"`
		LeadTimeDays int           `json:"lead_time_days"`
		Tiers        []supply.Tier `json:"tiers"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if body.Currency == "" {
		body.Currency = "CNY"
	}
	q := supply.Quote{
		OpportunityID: id, SupplierID: body.SupplierID, PartName: body.PartName,
		UnitPrice: body.UnitPrice, Currency: body.Currency, MOQ: body.MOQ,
		LeadTimeDays: body.LeadTimeDays, Tiers: body.Tiers,
	}
	created, err := s.st.CreateQuote(r.Context(), q)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "not found")
			return
		}
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, created)
}

// handleCompareQuotes GET /api/opportunities/{id}/quotes/compare?qty=1000
func (s *Server) handleCompareQuotes(w http.ResponseWriter, r *http.Request, _ *session) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	qty, err := strconv.Atoi(r.URL.Query().Get("qty"))
	if err != nil || qty <= 0 {
		writeErr(w, http.StatusBadRequest, "invalid qty")
		return
	}
	quotes, err := s.st.ListQuotes(r.Context(), id)
	if err != nil {
		writeServerErr(w, err)
		return
	}
	out, err := supply.Compare(quotes, qty)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// handleSelectQuote POST /api/opportunities/{id}/quotes/{qid}/select
func (s *Server) handleSelectQuote(w http.ResponseWriter, r *http.Request, _ *session) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	qid, err := strconv.ParseInt(r.PathValue("qid"), 10, 64)
	if err != nil || qid <= 0 {
		writeErr(w, http.StatusBadRequest, "invalid qid")
		return
	}
	if err := s.st.SelectQuote(r.Context(), id, qid); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "not found")
			return
		}
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleDeleteSupplier DELETE /api/suppliers?id=
func (s *Server) handleDeleteSupplier(w http.ResponseWriter, r *http.Request, _ *session) {
	id, err := strconv.ParseInt(r.URL.Query().Get("id"), 10, 64)
	if err != nil || id <= 0 {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := s.st.DeleteSupplier(r.Context(), id); err != nil {
		var quoted *store.ErrQuoted
		if errors.As(err, &quoted) {
			writeJSON(w, http.StatusConflict, map[string]any{
				"error": "供应商仍被报价引用，不能删除", "opportunity_ids": quoted.OpportunityIDs,
			})
			return
		}
		writeServerErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// decodeJSON 统一 JSON 解码：拒绝未知字段，错误体归一化。
func decodeJSON(r *http.Request, dst any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(dst)
}
