package handlers

import (
	"net/http"
	"strings"
	"time"

	"vozko/delivery/http/response"
	wc "vozko/domain/whatsapp_campaign"
	wce "vozko/domain/whatsapp_campaign_entry"
	"vozko/infra/http/middleware"
)

// parseSummaryDate parses a campaigns-summary date bound. It accepts a full
// RFC3339 timestamp (used as-is, timezone honored) or a date-only "2006-01-02"
// value. For a date-only upper bound, endOfDay extends it to the final instant
// of that day so the range stays inclusive. Returns nil for blank/unparseable
// input — an absent bound means unbounded.
func parseSummaryDate(raw string, endOfDay bool) *time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return &t
	}
	if t, err := time.Parse("2006-01-02", raw); err == nil {
		if endOfDay {
			t = t.Add(24*time.Hour - time.Nanosecond)
		}
		return &t
	}
	return nil
}

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
		CreatedFrom:   parseSummaryDate(values.Get("from"), false),
		CreatedTo:     parseSummaryDate(values.Get("to"), true),
	})
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to load whatsapp campaigns summary", nil)
		return
	}
	response.WriteSuccess(w, http.StatusOK, metrics)
}
