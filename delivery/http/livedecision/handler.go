package livedecision

import (
	"log"
	"net/http"
	"strconv"

	"vozko/delivery/http/response"
	livedecisions_usecase "vozko/usecases/livedecisions"
)

type Handler struct {
	service *livedecisions_usecase.Service
}

func NewHandler(service *livedecisions_usecase.Service) *Handler {
	return &Handler{service: service}
}

type SummaryResponse struct {
	WorkspaceID   string         `json:"workspaceId"`
	WorkspaceName string         `json:"workspaceName"`
	Decisions     int            `json:"decisions"`
	Failures      int            `json:"failures"`
	CostMicros    int64          `json:"costMicros"`
	AvgLatencyMs  int64          `json:"avgLatencyMs"`
	Effects       map[string]int `json:"effects"`
}

// Internal system-admin operation; intentionally excluded from public Swagger.
func (h *Handler) Summary(w http.ResponseWriter, r *http.Request) {
	days, err := strconv.Atoi(r.URL.Query().Get("days"))
	if err != nil {
		days = 7
	}
	summaries, err := h.service.Summarize(r.Context(), days)
	if err != nil {
		log.Printf("[live-decisions] summary failed: %v", err)
		response.WriteError(w, http.StatusInternalServerError, "Internal server error", nil)
		return
	}
	out := make([]SummaryResponse, 0, len(summaries))
	for _, s := range summaries {
		out = append(out, SummaryResponse{
			WorkspaceID:   s.WorkspaceID,
			WorkspaceName: s.WorkspaceName,
			Decisions:     s.Decisions,
			Failures:      s.Failures,
			CostMicros:    s.CostMicros,
			AvgLatencyMs:  s.AvgLatency.Milliseconds(),
			Effects:       s.Effects,
		})
	}
	response.WriteSuccess(w, http.StatusOK, out)
}
