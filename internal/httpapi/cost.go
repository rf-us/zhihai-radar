package httpapi

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/rf-us/zhihai-radar/internal/domain"
	"github.com/rf-us/zhihai-radar/internal/store"
)

// handleGetCost GET /api/opportunities/{id}/cost
func (s *Server) handleGetCost(w http.ResponseWriter, r *http.Request, _ *session) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	rec, err := s.st.LatestCost(r.Context(), id)
	if err != nil {
		writeServerErr(w, err)
		return
	}
	if rec == nil {
		writeJSON(w, http.StatusOK, map[string]any{"cost": nil})
		return
	}
	writeJSON(w, http.StatusOK, rec)
}

// handleSaveCost POST /api/opportunities/{id}/cost
//
// 请求体：{bom:[{part_name,qty,unit_price,loss_rate,use_mode}],
//
//	target_price?:number, competitor_prices?:number[]}
//
// 计算走 domain.EstimateCost（含 use_mode 除法），落库由 store.SaveCost 事务完成。
func (s *Server) handleSaveCost(w http.ResponseWriter, r *http.Request, _ *session) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	var body struct {
		BOM              []domain.CostLine `json:"bom"`
		TargetPrice      *float64          `json:"target_price"`
		CompetitorPrices []float64         `json:"competitor_prices"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(body.BOM) == 0 {
		writeErr(w, http.StatusBadRequest, "bom 至少一行")
		return
	}
	for i, line := range body.BOM {
		if line.UnitPrice < 0 {
			writeErr(w, http.StatusBadRequest, "bom["+strconv.Itoa(i)+"].unit_price 不能为负")
			return
		}
		if line.LossRate < 0 {
			writeErr(w, http.StatusBadRequest, "bom["+strconv.Itoa(i)+"].loss_rate 不能为负")
			return
		}
	}
	if body.TargetPrice != nil && *body.TargetPrice <= 0 {
		writeErr(w, http.StatusBadRequest, "target_price 必须大于 0")
		return
	}

	cb, ok := domain.EstimateCost(body.BOM, body.TargetPrice, body.CompetitorPrices)
	if !ok {
		writeErr(w, http.StatusBadRequest, "无法计算成本：BOM 全部无效")
		return
	}
	if err := s.st.SaveCost(r.Context(), id, body.BOM, cb); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "not found")
			return
		}
		writeServerErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"cost": cb, "opportunity_id": id})
}

// handleDeleteCost DELETE /api/opportunities/{id}/cost
func (s *Server) handleDeleteCost(w http.ResponseWriter, r *http.Request, _ *session) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := s.st.DeleteCost(r.Context(), id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "not found")
			return
		}
		writeServerErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
