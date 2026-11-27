package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/rf-us/zhihai-radar/internal/reef"
	"github.com/rf-us/zhihai-radar/internal/store"
)

// handleAdoptPreview POST /api/opportunities/{id}/adopt/preview
// 只读预检：将要创建什么、编码是否冲突、需要管理员权限。
func (s *Server) handleAdoptPreview(w http.ResponseWriter, r *http.Request, sess *session) {
	in, err := s.buildAdoptInput(r.Context(), sess, r.PathValue("id"))
	if err != nil {
		writeAdoptErr(w, err)
		return
	}
	out, err := s.reef.AdoptPreview(r.Context(), sess.token, in)
	if err != nil {
		writeReefErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// handleAdopt POST /api/opportunities/{id}/adopt
// 采纳 Saga：建产品 → 建零件（写供应商）→ 建 BOM → 记录 adoption。
func (s *Server) handleAdopt(w http.ResponseWriter, r *http.Request, sess *session) {
	if sess.user.Role != reef.RoleAdmin {
		writeErr(w, http.StatusForbidden, "采纳需要管理员账号")
		return
	}
	in, err := s.buildAdoptInput(r.Context(), sess, r.PathValue("id"))
	if err != nil {
		writeAdoptErr(w, err)
		return
	}
	res, err := s.reef.Adopt(r.Context(), sess.token, in)

	// 无论成败都记一条 adoption，便于回查（ok 有部分唯一索引挡重复）。
	status := "failed"
	if err == nil {
		status = res.Status
	}
	_ = s.st.SaveAdoption(r.Context(), &store.Adoption{
		OpportunityID: in.OpportunityID, ReefBase: s.cfg.ReefBase,
		ProductID: res.ProductID, BOMCreated: res.BOMCreated, BOMSkipped: res.BOMSkipped,
		Status: status, Message: res.Message, Operator: operatorOf(sess),
		CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		// Saga 已经尝试回滚；把状态与原因透给前端，不压成 500。
		writeJSON(w, http.StatusOK, res)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// buildAdoptInput 从机会、成本 BOM、选定报价组装采纳输入。
func (s *Server) buildAdoptInput(ctx context.Context, sess *session, rawID string) (reef.AdoptInput, error) {
	var out reef.AdoptInput
	id, err := strconv.ParseInt(rawID, 10, 64)
	if err != nil || id <= 0 {
		return out, errors.New("invalid id")
	}
	opp, err := s.st.GetOpportunity(ctx, id)
	if err != nil {
		return out, err
	}
	cost, err := s.st.LatestCost(ctx, id)
	if err != nil {
		return out, err
	}
	if cost == nil {
		return out, errors.New("尚未测算成本，不能采纳")
	}
	quotes, err := s.st.ListQuotes(ctx, id)
	if err != nil {
		return out, err
	}

	// 选定报价的供应商按零件名映射；没有选定报价时供应商留空。
	supplierOf := map[string]string{}
	for _, q := range quotes {
		if q.Selected && strings.TrimSpace(q.SupplierName) != "" {
			supplierOf[q.PartName] = q.SupplierName
		}
	}

	out = reef.AdoptInput{
		OpportunityID: id,
		Code:          fmt.Sprintf("OPP-%d", id),
		Name:          opp.Name,
		Unit:          "台",
	}
	seen := map[string]bool{}
	for _, line := range cost.BOM {
		name := strings.TrimSpace(line.PartName)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out.Parts = append(out.Parts, reef.PartSpec{
			Name: name, Code: fmt.Sprintf("P-%d-%d", id, len(out.Parts)+1),
			Unit: "个", Supplier: supplierOf[name],
		})
		out.Lines = append(out.Lines, reef.BOMLine{
			PartRef: name, Qty: line.Qty, LossRate: line.LossRate, UseMode: line.UseMode,
		})
	}
	if len(out.Lines) == 0 {
		return out, errors.New("成本 BOM 没有可用零件")
	}
	return out, nil
}

func writeAdoptErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeErr(w, http.StatusNotFound, "not found")
	default:
		writeErr(w, http.StatusBadRequest, err.Error())
	}
}
