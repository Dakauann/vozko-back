package crmboard

import (
	"errors"
	"net/http"
	"strings"

	"vozko/delivery/http/response"
	"vozko/domain/crmfilter"
	"vozko/domain/savedview"
	"vozko/infra/http/middleware"
	crmboard_usecase "vozko/usecases/crmboard"
)

type CRMBoardHandler struct {
	service *crmboard_usecase.Service
}

func NewCRMBoardHandler(service *crmboard_usecase.Service) *CRMBoardHandler {
	return &CRMBoardHandler{service: service}
}

// @Summary		Quadro do CRM
// @Description	Renderiza o quadro global do CRM do workspace, agrupado por etapa, etiqueta, responsável ou sem agrupamento, aplicando o filtro reutilizável, o pipeline e a paginação informados. Cada coluna traz suas conversas paginadas e o total.
// @Tags			CRM
// @Produce		json
// @Param			pipelineId	query	string	false	"ID do pipeline que escopa o quadro"
// @Param			groupBy		query	string	false	"Eixo de agrupamento ('stage', 'label', 'owner' ou 'none')"
// @Param			filter		query	string	false	"Filtro crmfilter em JSON (opcionalmente codificado em base64)"
// @Param			owners		query	string	false	"Colunas do eixo responsável em JSON com id e nome (opcionalmente em base64)"
// @Param			sortField	query	string	false	"Campo de ordenação"
// @Param			sortOrder	query	string	false	"Direção da ordenação ('asc' ou 'desc')"
// @Param			page		query	int		false	"Número da página (inicia em 1)"
// @Param			pageSize	query	int		false	"Tamanho da página (máximo 200)"
// @Success		200	{object}	crmboard_usecase.Board
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/crm/board [get]
func (h *CRMBoardHandler) Board(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}
	q := r.URL.Query()

	filter, err := decodeFilterParam(q.Get("filter"))
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, "invalid filter parameter", nil)
		return
	}
	owners, err := decodeOwnersParam(q.Get("owners"))
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, "invalid owners parameter", nil)
		return
	}
	page, pageSize := crmBoardPagination(q)

	board, err := h.service.GetBoard(crmboard_usecase.BoardInput{
		WorkspaceID:          middleware.GetWorkspaceID(r),
		UserID:               claims.UserID,
		IsAdmin:              claims.Role == "admin",
		SelectedDepartmentID: selectedDepartmentID(r),
		PipelineID:           strings.TrimSpace(q.Get("pipelineId")),
		GroupBy:              savedview.GroupBy(strings.TrimSpace(q.Get("groupBy"))),
		Filter:               filter,
		Owners:               owners,
		SortField:            strings.TrimSpace(q.Get("sortField")),
		SortOrder:            strings.TrimSpace(q.Get("sortOrder")),
		Page:                 page,
		PageSize:             pageSize,
	})
	if err != nil {
		h.handleError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, board)
}

// @Summary		Lista plana do CRM
// @Description	Retorna a lista plana de conversas do CRM sobre o mesmo filtro reutilizável do quadro, com ordenação e paginação. A resposta traz as entradas e o total.
// @Tags			CRM
// @Produce		json
// @Param			filter		query	string	false	"Filtro crmfilter em JSON (opcionalmente codificado em base64)"
// @Param			sortField	query	string	false	"Campo de ordenação"
// @Param			sortOrder	query	string	false	"Direção da ordenação ('asc' ou 'desc')"
// @Param			page		query	int		false	"Número da página (inicia em 1)"
// @Param			pageSize	query	int		false	"Tamanho da página (máximo 200)"
// @Success		200	{object}	map[string]interface{}
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/crm/entries [get]
func (h *CRMBoardHandler) Entries(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}
	q := r.URL.Query()

	filter, err := decodeFilterParam(q.Get("filter"))
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, "invalid filter parameter", nil)
		return
	}
	page, pageSize := crmBoardPagination(q)

	entries, total, err := h.service.GetEntries(crmboard_usecase.EntriesInput{
		WorkspaceID:          middleware.GetWorkspaceID(r),
		UserID:               claims.UserID,
		IsAdmin:              claims.Role == "admin",
		SelectedDepartmentID: selectedDepartmentID(r),
		Filter:               filter,
		SortField:            strings.TrimSpace(q.Get("sortField")),
		SortOrder:            strings.TrimSpace(q.Get("sortOrder")),
		Page:                 page,
		PageSize:             pageSize,
	})
	if err != nil {
		h.handleError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, map[string]interface{}{
		"entries": entries,
		"total":   total,
	})
}

func (h *CRMBoardHandler) handleError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, crmboard_usecase.ErrUnauthorized):
		response.WriteError(w, http.StatusForbidden, err.Error(), nil)
	case errors.Is(err, crmboard_usecase.ErrUnsupportedGroupBy),
		errors.Is(err, crmfilter.ErrUnknownField),
		errors.Is(err, crmfilter.ErrUnsupportedOp),
		errors.Is(err, crmfilter.ErrMissingValue),
		errors.Is(err, crmfilter.ErrBetweenValues),
		errors.Is(err, crmfilter.ErrInvalidNumber),
		errors.Is(err, crmfilter.ErrInvalidDate),
		errors.Is(err, crmfilter.ErrMissingCustomKey):
		response.WriteError(w, http.StatusBadRequest, err.Error(), nil)
	default:
		response.WriteError(w, http.StatusInternalServerError, "Internal server error", nil)
	}
}
