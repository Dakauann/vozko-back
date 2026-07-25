package issue

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gorilla/mux"

	"vozko/delivery/http/response"
	issuesdomain "vozko/domain/issues"
	"vozko/domain/issues/issue_response"
	"vozko/domain/shared"
	"vozko/infra/http/middleware"
)

type IssueHandler struct {
	createUseCase         issuesdomain.CreateIssueUseCase
	listUseCase           issuesdomain.ListIssuesUseCase
	listAllUseCase        issuesdomain.ListAllIssuesUseCase
	getUseCase            issuesdomain.GetIssueUseCase
	closeUseCase          issuesdomain.CloseIssueUseCase
	updateStatusUseCase   issuesdomain.UpdateIssueStatusUseCase
	createResponseUseCase issue_response.CreateResponseUseCase
	listResponsesUseCase  issue_response.ListResponsesUseCase
}

func NewIssueHandler(
	createUC issuesdomain.CreateIssueUseCase,
	listUC issuesdomain.ListIssuesUseCase,
	listAllUC issuesdomain.ListAllIssuesUseCase,
	getUC issuesdomain.GetIssueUseCase,
	closeUC issuesdomain.CloseIssueUseCase,
	updateStatusUC issuesdomain.UpdateIssueStatusUseCase,
	createResponseUC issue_response.CreateResponseUseCase,
	listResponsesUC issue_response.ListResponsesUseCase,
) *IssueHandler {
	return &IssueHandler{
		createUseCase:         createUC,
		listUseCase:           listUC,
		listAllUseCase:        listAllUC,
		getUseCase:            getUC,
		closeUseCase:          closeUC,
		updateStatusUseCase:   updateStatusUC,
		createResponseUseCase: createResponseUC,
		listResponsesUseCase:  listResponsesUC,
	}
}

// @Summary		Criar chamado
// @Description	Cria um novo chamado (report de problema ou sugestão) no workspace. O título é obrigatório e é possível anexar imagens.
// @Tags			Chamados
// @Accept			json
// @Produce		json
// @Param			request	body		CreateIssueRequest	true	"Dados do chamado a criar"
// @Success		201	{object}	issues.Issue
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/issues [post]
func (h *IssueHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req CreateIssueRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, "Invalid request body", nil)
		return
	}

	wsID := middleware.GetWorkspaceID(r)

	created, err := h.createUseCase.Execute(wsID, req.Title, req.Description, req.ImageURLs)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}

	response.WriteSuccess(w, http.StatusCreated, created)
}

// @Summary		Listar chamados
// @Description	Retorna os chamados do workspace, com paginação, ordenação e filtros por status e busca textual.
// @Tags			Chamados
// @Produce		json
// @Param			page		query	int		false	"Número da página"
// @Param			pageSize	query	int		false	"Itens por página"
// @Param			sort		query	string	false	"Ordenação (ex.: createdAt:desc)"
// @Param			status		query	string	false	"Filtrar por status (separado por vírgula)"
// @Param			search		query	string	false	"Busca textual"
// @Success		200	{array}		issues.Issue
// @Failure		401	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/issues [get]
func (h *IssueHandler) List(w http.ResponseWriter, r *http.Request) {
	wsID := middleware.GetWorkspaceID(r)
	query := r.URL.Query()

	input := issuesdomain.ListIssuesInput{
		WorkspaceID: wsID,
		Options: shared.QueryOptions{
			Pagination: parsePagination(query),
			Sorts:      parseSort(query, map[string]string{"createdat": "createdAt", "updatedat": "updatedAt", "status": "status", "title": "title"}),
			Filters:    parseIssueFilters(query),
		},
	}

	result, err := h.listUseCase.Execute(input)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to list issues", nil)
		return
	}

	response.WritePaginated(w, http.StatusOK, result.Items, response.PaginationMeta{
		Page:       result.Page,
		PageSize:   result.PageSize,
		TotalPages: result.TotalPages,
		TotalItems: result.TotalItems,
	})
}

// @Summary		Obter chamado
// @Description	Retorna os detalhes de um chamado específico pelo seu ID.
// @Tags			Chamados
// @Produce		json
// @Param			id	path	string	true	"ID do chamado"
// @Success		200	{object}	issues.Issue
// @Failure		401	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/issues/{id} [get]
func (h *IssueHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]

	issue, err := h.getUseCase.Execute(id)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}

	response.WriteSuccess(w, http.StatusOK, issue)
}

// @Summary		Encerrar chamado
// @Description	Marca um chamado como encerrado. Chamados já encerrados não podem ser reabertos.
// @Tags			Chamados
// @Produce		json
// @Param			id	path	string	true	"ID do chamado"
// @Success		200	{object}	issues.Issue
// @Failure		401	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Failure		409	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/issues/{id}/close [patch]
func (h *IssueHandler) Close(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]

	issue, err := h.closeUseCase.Execute(id)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}

	response.WriteSuccess(w, http.StatusOK, issue)
}

func (h *IssueHandler) ListAll(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()

	options := shared.QueryOptions{
		Pagination: parsePagination(query),
		Sorts:      parseSort(query, map[string]string{"createdat": "createdAt", "updatedat": "updatedAt", "status": "status", "title": "title"}),
		Filters:    parseIssueFilters(query),
	}

	result, err := h.listAllUseCase.Execute(options)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to list issues", nil)
		return
	}

	response.WritePaginated(w, http.StatusOK, result.Items, response.PaginationMeta{
		Page:       result.Page,
		PageSize:   result.PageSize,
		TotalPages: result.TotalPages,
		TotalItems: result.TotalItems,
	})
}

func (h *IssueHandler) UpdateStatus(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]

	var req UpdateIssueStatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, "Invalid request body", nil)
		return
	}

	status := issuesdomain.IssueStatus(req.Status)
	issue, err := h.updateStatusUseCase.Execute(id, status)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}

	response.WriteSuccess(w, http.StatusOK, issue)
}

func (h *IssueHandler) handleDomainError(w http.ResponseWriter, err error) {
	switch err {
	case issuesdomain.ErrIssueNotFound:
		response.WriteError(w, http.StatusNotFound, err.Error(), nil)
	case issuesdomain.ErrIssueTitleRequired:
		response.WriteValidationError(w, map[string]string{"title": "required"})
	case issuesdomain.ErrIssueTitleTooLong:
		response.WriteValidationError(w, map[string]string{"title": "must not exceed 255 characters"})
	case issuesdomain.ErrIssueAlreadyClosed:
		response.WriteError(w, http.StatusConflict, err.Error(), nil)
	case issuesdomain.ErrInvalidStatus:
		response.WriteValidationError(w, map[string]string{"status": "invalid issue status"})
	default:
		response.WriteError(w, http.StatusInternalServerError, "Internal server error", nil)
	}
}

func parseIssueFilters(values url.Values) []shared.Filter {
	var filters []shared.Filter

	if v := strings.TrimSpace(values.Get("status")); v != "" {
		filters = append(filters, shared.Filter{
			Field:  "status",
			Values: strings.Split(v, ","),
		})
	}

	if v := strings.TrimSpace(values.Get("search")); v != "" {
		filters = append(filters, shared.Filter{
			Field:  "search",
			Values: []string{v},
		})
	}

	return filters
}

// @Summary		Responder a um chamado
// @Description	Adiciona uma resposta a um chamado existente. O corpo é obrigatório e é possível anexar uma imagem.
// @Tags			Chamados
// @Accept			json
// @Produce		json
// @Param			id		path		string						true	"ID do chamado"
// @Param			request	body		CreateIssueResponseRequest	true	"Dados da resposta"
// @Success		201	{object}	issue_response.IssueResponse
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/issues/{id}/responses [post]
func (h *IssueHandler) CreateResponse(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]

	var req CreateIssueResponseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, "Invalid request body", nil)
		return
	}

	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	resp, err := h.createResponseUseCase.Execute(id, claims.UserID, req.Body, req.ImageURL)
	if err != nil {
		switch err {
		case issue_response.ErrIssueNotFound:
			response.WriteError(w, http.StatusNotFound, err.Error(), nil)
		case issue_response.ErrResponseBodyEmpty:
			response.WriteValidationError(w, map[string]string{"body": "required"})
		case issue_response.ErrResponseBodyTooLong:
			response.WriteValidationError(w, map[string]string{"body": "must not exceed 2000 characters"})
		default:
			response.WriteError(w, http.StatusInternalServerError, "Internal server error", nil)
		}
		return
	}

	response.WriteSuccess(w, http.StatusCreated, resp)
}

// @Summary		Listar respostas do chamado
// @Description	Retorna as respostas (mensagens) associadas a um chamado, em ordem cronológica.
// @Tags			Chamados
// @Produce		json
// @Param			id	path	string	true	"ID do chamado"
// @Success		200	{array}		issue_response.IssueResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/issues/{id}/responses [get]
func (h *IssueHandler) ListResponses(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]

	responses, err := h.listResponsesUseCase.Execute(id)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to list responses", nil)
		return
	}

	response.WriteSuccess(w, http.StatusOK, responses)
}

func parsePagination(values url.Values) shared.Pagination {
	page := 1
	if v := strings.TrimSpace(values.Get("page")); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 {
			page = parsed
		}
	}

	pageSize := shared.DefaultPageSize
	if v := strings.TrimSpace(values.Get("pageSize")); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 {
			pageSize = parsed
		}
	}

	return shared.Pagination{Page: page, PageSize: pageSize}
}

func parseSort(values url.Values, allowed map[string]string) []shared.Sort {
	rawSorts := values["sort"]
	if len(rawSorts) == 0 {
		return nil
	}

	sorts := make([]shared.Sort, 0)
	for _, raw := range rawSorts {
		entries := strings.Split(raw, ",")
		for _, entry := range entries {
			entry = strings.TrimSpace(entry)
			if entry == "" {
				continue
			}
			parts := strings.Split(entry, ":")
			fieldKey := strings.ToLower(strings.TrimSpace(parts[0]))
			field, ok := allowed[fieldKey]
			if !ok {
				continue
			}

			direction := shared.SortAsc
			if len(parts) > 1 {
				if dir := strings.ToLower(strings.TrimSpace(parts[1])); dir == string(shared.SortDesc) {
					direction = shared.SortDesc
				}
			}

			sorts = append(sorts, shared.Sort{Field: field, Direction: direction})
		}
	}

	return sorts
}
