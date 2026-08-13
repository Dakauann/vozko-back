package handlers

import (
	"net/http"
	"strings"

	"vozko/delivery/http/httpx"
	"vozko/delivery/http/response"
	wc "vozko/domain/whatsapp_campaign"
	wce "vozko/domain/whatsapp_campaign_entry"
	"vozko/infra/http/middleware"
)

// Summary returns the workspace-level "disparos" rollup for WhatsApp campaigns,
// filtered by campaign creation date (from/to), type and department. With no
// date params it returns the all-time totals.
func (h *WhatsAppCampaignHandler) Summary(w http.ResponseWriter, r *http.Request) {
	if middleware.GetClaims(r) == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	if shouldReturnEmptyDepartmentList(r) {
		response.WriteSuccess(w, http.StatusOK, wc.NewCampaignMetrics(nil))
		return
	}

	values := r.URL.Query()
	metrics, err := h.summaryUseCase.Execute(wce.WorkspaceSummaryFilter{
		WorkspaceID:   middleware.GetWorkspaceID(r),
		DepartmentIDs: departmentFilterIDs(r),
		Type:          strings.TrimSpace(values.Get("type")),
		CreatedFrom:   httpx.ParseDateBound(values.Get("from"), false),
		CreatedTo:     httpx.ParseDateBound(values.Get("to"), true),
	})
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to load whatsapp campaigns summary", nil)
		return
	}
	response.WriteSuccess(w, http.StatusOK, metrics)
}
