package opportunityboard

import (
	"errors"
	"net/http"
	"strings"

	"vozko/delivery/http/response"
	"vozko/domain/crmfilter"
	"vozko/domain/savedview"
	"vozko/infra/http/middleware"
	oppboard_usecase "vozko/usecases/oppboard"
)

type OpportunityBoardHandler struct {
	service *oppboard_usecase.Service
}

func NewOpportunityBoardHandler(service *oppboard_usecase.Service) *OpportunityBoardHandler {
	return &OpportunityBoardHandler{service: service}
}

// @Summary		Board de oportunidades (Funil de Vendas)
// @Description	Renderiza o funil de vendas do workspace agrupado por etapa, responsável ou campo personalizado. Cada coluna traz a contagem de oportunidades e a soma dos valores (em reais do cliente). Respeita o escopo de departamento do usuário.
// @Tags			Oportunidades
// @Produce		json
// @Param			pipelineId	query		string	false	"ID do pipeline a exibir"
// @Param			groupBy		query		string	false	"Eixo de agrupamento das colunas ('stage', 'owner' ou 'custom')"
// @Param			groupByKey	query		string	false	"Chave do campo personalizado quando groupBy=custom"
// @Param			filter		query		string	false	"Filtro compartilhado (JSON crmfilter, opcionalmente em base64)"
// @Param			owners		query		string	false	"Colunas do eixo de responsáveis (JSON, opcionalmente em base64)"
// @Param			options		query		string	false	"Colunas do eixo de campo personalizado (JSON, opcionalmente em base64)"
// @Param			sortField	query		string	false	"Campo de ordenação das oportunidades"
// @Param			sortOrder	query		string	false	"Direção da ordenação ('asc' ou 'desc')"
// @Param			page		query		int		false	"Página das oportunidades por coluna"
// @Param			pageSize	query		int		false	"Tamanho da página (máximo 200)"
// @Success		200	{object}	oppboard_usecase.Board
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/opportunities/board [get]
func (h *OpportunityBoardHandler) Board(w http.ResponseWriter, r *http.Request) {
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
	owners, err := decodeOppOwnersParam(q.Get("owners"))
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, "invalid owners parameter", nil)
		return
	}
	options, err := decodeOppOptionsParam(q.Get("options"))
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, "invalid options parameter", nil)
		return
	}
	page, pageSize := crmBoardPagination(q)

	board, err := h.service.GetBoard(oppboard_usecase.BoardInput{
		WorkspaceID: middleware.GetWorkspaceID(r),
		UserID:      claims.UserID,
		IsAdmin:     claims.Role == "admin",
		PipelineID:  strings.TrimSpace(q.Get("pipelineId")),
		GroupBy:     savedview.GroupBy(strings.TrimSpace(q.Get("groupBy"))),
		GroupByKey:  strings.TrimSpace(q.Get("groupByKey")),
		Filter:      filter,
		Owners:      owners,
		Options:     options,
		SortField:   strings.TrimSpace(q.Get("sortField")),
		SortOrder:   strings.TrimSpace(q.Get("sortOrder")),
		Page:        page,
		PageSize:    pageSize,
	})
	if err != nil {
		h.handleError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, board)
}

// @Summary		Lista de oportunidades
// @Description	Lista plana das oportunidades do workspace sobre o mesmo filtro do funil, com ordenação e paginação. Respeita o escopo de departamento do usuário. Retorna as oportunidades e o total de correspondências.
// @Tags			Oportunidades
// @Produce		json
// @Param			filter		query		string	false	"Filtro compartilhado (JSON crmfilter, opcionalmente em base64)"
// @Param			sortField	query		string	false	"Campo de ordenação"
// @Param			sortOrder	query		string	false	"Direção da ordenação ('asc' ou 'desc')"
// @Param			sort		query		string	false	"Ordenação combinada no formato 'campo:direção' (ex.: 'value:desc')"
// @Param			page		query		int		false	"Página"
// @Param			pageSize	query		int		false	"Tamanho da página (máximo 200)"
// @Success		200	{array}		opportunity.Opportunity
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/opportunities/list [get]
func (h *OpportunityBoardHandler) List(w http.ResponseWriter, r *http.Request) {
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
	sortField, sortOrder := oppSortParams(q)
	page, pageSize := crmBoardPagination(q)

	opportunities, total, err := h.service.GetList(oppboard_usecase.ListInput{
		WorkspaceID: middleware.GetWorkspaceID(r),
		UserID:      claims.UserID,
		IsAdmin:     claims.Role == "admin",
		Filter:      filter,
		SortField:   sortField,
		SortOrder:   sortOrder,
		Page:        page,
		PageSize:    pageSize,
	})
	if err != nil {
		h.handleError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, map[string]interface{}{
		"opportunities": opportunities,
		"total":         total,
	})
}

func (h *OpportunityBoardHandler) handleError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, oppboard_usecase.ErrUnauthorized):
		response.WriteError(w, http.StatusForbidden, err.Error(), nil)
	case errors.Is(err, oppboard_usecase.ErrUnsupportedGroupBy),
		errors.Is(err, oppboard_usecase.ErrGroupByKeyMissing),
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
