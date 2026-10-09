package crmbulk

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"vozko/delivery/http/httpx"
	"vozko/delivery/http/response"
	"vozko/domain/crmfilter"
	"vozko/domain/selection"
	"vozko/infra/http/middleware"
	crmbulk_usecase "vozko/usecases/crmbulk"
)

type Service interface {
	BulkApply(ctx context.Context, in crmbulk_usecase.BulkInput) (crmbulk_usecase.BulkResult, error)
	Count(ctx context.Context, scope selection.Scope, filter crmfilter.Filter) (int, string, error)
}

type CRMBulkHandler struct {
	service Service
}

func NewCRMBulkHandler(service Service) *CRMBulkHandler {
	return &CRMBulkHandler{service: service}
}

// @Summary		Aplicar uma ação em massa no CRM
// @Description	Aplica uma única ação (mover de etapa, mover de funil, atribuir, adicionar ou remover etiqueta) às conversas selecionadas. A seleção tem exatamente um modo: ids (targets escolhidos), all_matching (um filtro não vazio) ou everyone (todo o escopo do usuário, só com expectedCount). Para all_matching e everyone, fingerprint vem de POST /crm/bulk/count e é obrigatório; expectedCount é o matched dessa contagem, tomada antes das exclusões: excludeIds tira conversas da seleção sem mudar a contagem confirmada. Se a contagem mudou, responde 409 selection_changed com a nova contagem. targets junto com all_matching ou everyone é recusado (selection_ambiguous). Sem mode, targets valem como ids, um filtro não vazio como all_matching e um filtro vazio como everyone, com as mesmas exigências. Um departamento fora do escopo do usuário responde 403 selection_scope_denied; um filtro que não se aplica a conversas responde 400 invalid_filter. A contagem passa pelo limitador de agregados: ocupado, responde 503 com Retry-After. No máximo 2000 conversas por chamada (truncated indica que restam outras), em ordem estável por id.
// @Tags			CRM
// @Accept			json
// @Produce		json
// @Param			body	body		BulkApplyRequest	true	"Ação, seleção e valor"
// @Success		200	{object}	BulkResultResponse
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		409	{object}	SelectionChangedResponse
// @Failure		503	{object}	response.ErrorResponse
// @Header		503	{string}	Retry-After	"Segundos até tentar de novo"
// @Security		BearerAuth
// @Router			/crm/bulk [post]
func (h *CRMBulkHandler) Bulk(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := httpx.RequireWorkspace(w, r)
	if !ok {
		return
	}
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}
	var req BulkApplyRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}

	result, err := h.service.BulkApply(r.Context(), crmbulk_usecase.BulkInput{
		WorkspaceID:          workspaceID,
		ActorID:              claims.UserID,
		IsAdmin:              claims.Role == "admin",
		Action:               strings.TrimSpace(req.Action),
		Value:                strings.TrimSpace(req.Value),
		Targets:              req.targets(),
		Selection:            req.selection(),
		SelectedDepartmentID: middleware.SelectedDepartmentID(r),
	})
	if err != nil {
		writeBulkError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, toBulkResultResponse(result))
}

// @Summary		Contar a seleção de uma ação em massa no CRM
// @Description	Conta as conversas que o filtro alcança dentro do escopo do usuário (departamentos e atribuições), sem cache, e devolve o fingerprint do filtro contado. Filtro vazio conta todo o escopo (modo everyone). A contagem ignora exclusões: envie matched como expectedCount, o fingerprint e os excludeIds em POST /crm/bulk. Passa pelo limitador de agregados: ocupado, responde 503 com Retry-After.
// @Tags			CRM
// @Accept			json
// @Produce		json
// @Param			body	body		BulkCountRequest	true	"Filtro"
// @Success		200	{object}	BulkCountResponse
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		503	{object}	response.ErrorResponse
// @Header		503	{string}	Retry-After	"Segundos até tentar de novo"
// @Security		BearerAuth
// @Router			/crm/bulk/count [post]
func (h *CRMBulkHandler) Count(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := httpx.RequireWorkspace(w, r)
	if !ok {
		return
	}
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}
	var req BulkCountRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	filter := crmfilter.Filter{}
	if req.Filter != nil {
		filter = *req.Filter
	}

	matched, fingerprint, err := h.service.Count(r.Context(), selection.Scope{
		WorkspaceID:  workspaceID,
		ActorID:      claims.UserID,
		DepartmentID: middleware.SelectedDepartmentID(r),
		IsAdmin:      claims.Role == "admin",
	}, filter)
	if err != nil {
		writeBulkError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, BulkCountResponse{Matched: matched, Fingerprint: fingerprint})
}

func writeBulkError(w http.ResponseWriter, err error) {
	if httpx.WriteAnalyticsLimit(w, err, "Bulk selection") {
		return
	}
	var changed *selection.CountChangedError
	switch {
	case errors.As(err, &changed):
		response.WriteSuccess(w, http.StatusConflict, SelectionChangedResponse{
			Error: true, Code: selection.ErrorCode(err), Message: "A seleção mudou desde a contagem",
			Expected: changed.Expected, Matched: changed.Matched,
		})
	case errors.Is(err, crmbulk_usecase.ErrForbidden):
		response.WriteErrorWithCode(w, http.StatusForbidden, "forbidden", "You don't have permission to perform this bulk action", nil)
	case errors.Is(err, selection.ErrScopeDenied):
		response.WriteErrorWithCode(w, http.StatusForbidden, selection.ErrorCode(err), "The selection is outside your department scope", nil)
	case errors.Is(err, crmbulk_usecase.ErrUnknownAction):
		response.WriteErrorWithCode(w, http.StatusBadRequest, "unknown_action", "Unknown bulk action", map[string]string{
			"action": "move_stage | move_funnel | assign | add_label | remove_label",
		})
	case errors.Is(err, selection.ErrResolverUnavailable):
		response.WriteErrorWithCode(w, http.StatusServiceUnavailable, selection.ErrorCode(err), "Selection by filter is unavailable, retry later", nil)
	case selection.ErrorCode(err) != "":
		response.WriteErrorWithCode(w, http.StatusBadRequest, selection.ErrorCode(err), err.Error(), nil)
	default:
		response.WriteError(w, http.StatusInternalServerError, "Failed to apply the bulk action", nil)
	}
}
