package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/rf-us/zhihai-radar/internal/report"
	"github.com/rf-us/zhihai-radar/internal/store"
	"github.com/rf-us/zhihai-radar/internal/supply"
)

// handleExportReport GET /api/opportunities/{id}/report?sections=compliance,logistics
//
// 报告由已落库数据现场组装，生成时不重新调知乎接口——同一份材料两次生成结果一致。
func (s *Server) handleExportReport(w http.ResponseWriter, r *http.Request, _ *session) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	ctx := r.Context()

	opp, err := s.st.GetOpportunity(ctx, id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "not found")
			return
		}
		writeServerErr(w, err)
		return
	}
	evidence, err := s.st.ListEvidence(ctx, id)
	if err != nil {
		writeServerErr(w, err)
		return
	}
	cost, err := s.st.LatestCost(ctx, id)
	if err != nil {
		writeServerErr(w, err)
		return
	}
	quotes, err := s.st.ListQuotes(ctx, id)
	if err != nil {
		writeServerErr(w, err)
		return
	}

	var sections []string
	if raw := strings.TrimSpace(r.URL.Query().Get("sections")); raw != "" {
		for _, key := range strings.Split(raw, ",") {
			if key = strings.TrimSpace(key); key != "" {
				sections = append(sections, key)
			}
		}
	}

	// 有报价时用默认目标数量 1000 算比价，供报告表格展示。
	var comparison []supply.Comparison
	if len(quotes) > 0 {
		comparison, err = supply.Compare(quotes, 1000)
		if err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
	}

	doc := report.Assemble(report.AssembleInput{
		Opportunity: *opp, Evidence: evidence, Cost: cost,
		Quotes: quotes, Comparison: comparison, Sections: sections,
		Now: s.now(),
	})
	res, err := report.Render(doc)
	if err != nil {
		writeServerErr(w, err)
		return
	}

	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", "attachment; filename=report-"+strconv.FormatInt(id, 10)+".pdf")
	w.Header().Set("X-Missing-Runes", strconv.Itoa(res.MissingCount))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(res.PDF)
}
