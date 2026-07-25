package crmbulk

import (
	"encoding/json"
	"net/http"
	"strings"

	"vozko/delivery/http/response"
	"vozko/infra/http/middleware"
	crmbulk_usecase "vozko/usecases/crmbulk"
)

type CRMBulkHandler struct {
	service *crmbulk_usecase.Service
}

func NewCRMBulkHandler(service *crmbulk_usecase.Service) *CRMBulkHandler {
	return &CRMBulkHandler{service: service}
}

// @Summary		Aplicar uma ação em massa no CRM
// @Description	Aplica uma única ação (mover de etapa, atribuir, adicionar ou remover etiqueta) a várias conversas de uma só vez.
// @Tags			CRM
// @Accept			json
// @Produce		json
// @Param			body	body		BulkApplyRequest	true	"Ação, alvos e valor"
// @Success		200	{object}	BulkResultResponse
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/crm/bulk [post]
func (h *CRMBulkHandler) Bulk(w http.ResponseWriter, r *http.Request) {
	var req BulkApplyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, "Invalid request body", map[string]string{
			"action":  "string (required: move_stage | assign | add_label | remove_label)",
			"targets": "[]{entryId, entryType} (required)",
			"value":   "string (required: stageId | userId | labelId)",
		})
		return
	}

	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	if strings.TrimSpace(req.Action) == "" || len(req.Targets) == 0 {
		response.WriteError(w, http.StatusBadRequest, "action and targets are required", nil)
		return
	}

	targets := make([]crmbulk_usecase.EntryRef, 0, len(req.Targets))
	for _, t := range req.Targets {
		targets = append(targets, crmbulk_usecase.EntryRef{
			EntryID:   strings.TrimSpace(t.EntryID),
			EntryType: strings.TrimSpace(t.EntryType),
		})
	}

	result := h.service.BulkApply(r.Context(), crmbulk_usecase.BulkInput{
		WorkspaceID: middleware.GetWorkspaceID(r),
		ActorID:     claims.UserID,
		IsAdmin:     claims.Role == "admin",
		Action:      strings.TrimSpace(req.Action),
		Targets:     targets,
		Value:       strings.TrimSpace(req.Value),
	})

	if result.Forbidden {
		response.WriteError(w, http.StatusForbidden,
			"You don't have permission to perform this bulk action", map[string]string{
				"action": req.Action,
			})
		return
	}

	response.WriteSuccess(w, http.StatusOK, toBulkResultResponse(result))
}
