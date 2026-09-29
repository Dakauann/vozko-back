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

// @Summary		Resumo das decisões em tempo real
// @Description	Por workspace: quantas decisões, falhas, custo, latência média e etapas movidas no período. Apenas administradores da plataforma.
// @Tags			Decisões em tempo real
// @Produce		json
// @Param			days	query	int	false	"Dias para trás (1 a 90, padrão 7)"
// @Success		200	{array}	SummaryResponse
// @Security		BearerAuth
// @Router			/admin/live-decisions/summary [get]
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
