package supportinbox

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"github.com/gorilla/mux"

	"vozko/delivery/http/httpx"
	"vozko/delivery/http/response"
	"vozko/domain/shared"
	supportinboxdomain "vozko/domain/support_inbox"
	"vozko/infra/http/middleware"
)

type SupportInboxHandler struct {
	createUC           supportinboxdomain.CreateInboxUseCase
	updateUC           supportinboxdomain.UpdateInboxUseCase
	deleteUC           supportinboxdomain.DeleteInboxUseCase
	getUC              supportinboxdomain.GetInboxUseCase
	listUC             supportinboxdomain.ListInboxesUseCase
	createSessionUC    supportinboxdomain.CreateSessionUseCase
	reconnectSessionUC supportinboxdomain.ReconnectSessionUseCase
}

func NewSupportInboxHandler(
	createUC supportinboxdomain.CreateInboxUseCase,
	updateUC supportinboxdomain.UpdateInboxUseCase,
	deleteUC supportinboxdomain.DeleteInboxUseCase,
	getUC supportinboxdomain.GetInboxUseCase,
	listUC supportinboxdomain.ListInboxesUseCase,
	createSessionUC supportinboxdomain.CreateSessionUseCase,
	reconnectSessionUC supportinboxdomain.ReconnectSessionUseCase,
) *SupportInboxHandler {
	return &SupportInboxHandler{
		createUC:           createUC,
		updateUC:           updateUC,
		deleteUC:           deleteUC,
		getUC:              getUC,
		listUC:             listUC,
		createSessionUC:    createSessionUC,
		reconnectSessionUC: reconnectSessionUC,
	}
}

// @Summary		Criar caixa de atendimento
// @Description	Cria uma nova caixa de atendimento (widget de chat incorporável) no workspace.
// @Tags			Caixas de atendimento
// @Accept			json
// @Produce		json
// @Param			body	body		SupportInboxRequest	true	"Dados da caixa de atendimento"
// @Success		201	{object}	SupportInboxResponse
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/support/inboxes [post]
func (h *SupportInboxHandler) Create(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	var req SupportInboxRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteInvalidBodyError(w, map[string]string{
			"name":           "string (required)",
			"allowedOrigins": "array of strings (e.g. [\"https://mysite.com\"])",
		})
		return
	}

	inbox := &supportinboxdomain.SupportInbox{
		WorkspaceID:          middleware.GetWorkspaceID(r),
		Name:                 req.Name,
		AgentID:              req.AgentID,
		EnableAgentResponses: req.EnableAgentResponses,
		GreetingMessage:      req.GreetingMessage,
		WidgetColor:          req.WidgetColor,
		AllowedOrigins:       req.AllowedOrigins,
		PreChatFields:        toDomainPreChatFields(req.PreChatFields),
	}

	created, err := h.createUC.Execute(inbox)
	if err != nil {
		handleSupportInboxError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusCreated, toSupportInboxResponse(created))
}

// @Summary		Atualizar caixa de atendimento
// @Description	Atualiza os dados de uma caixa de atendimento existente.
// @Tags			Caixas de atendimento
// @Accept			json
// @Produce		json
// @Param			id		path		string				true	"Identificador da caixa de atendimento"
// @Param			body	body		SupportInboxRequest	true	"Dados da caixa de atendimento"
// @Success		200	{object}	SupportInboxResponse
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/support/inboxes/{id} [put]
func (h *SupportInboxHandler) Update(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	id := mux.Vars(r)["id"]
	if id == "" {
		response.WriteError(w, http.StatusBadRequest, "Missing inbox id", nil)
		return
	}

	var req SupportInboxRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, "Invalid request body", nil)
		return
	}

	inbox := &supportinboxdomain.SupportInbox{
		Name:                 req.Name,
		AgentID:              req.AgentID,
		EnableAgentResponses: req.EnableAgentResponses,
		GreetingMessage:      req.GreetingMessage,
		WidgetColor:          req.WidgetColor,
		AllowedOrigins:       req.AllowedOrigins,
		PreChatFields:        toDomainPreChatFields(req.PreChatFields),
	}

	updated, err := h.updateUC.Execute(id, inbox)
	if err != nil {
		handleSupportInboxError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, toSupportInboxResponse(updated))
}

// @Summary		Remover caixa de atendimento
// @Description	Remove uma caixa de atendimento do workspace.
// @Tags			Caixas de atendimento
// @Produce		json
// @Param			id	path		string	true	"Identificador da caixa de atendimento"
// @Success		200	{object}	map[string]string
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/support/inboxes/{id} [delete]
func (h *SupportInboxHandler) Delete(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	id := mux.Vars(r)["id"]
	if id == "" {
		response.WriteError(w, http.StatusBadRequest, "Missing inbox id", nil)
		return
	}

	if err := h.deleteUC.Execute(id); err != nil {
		handleSupportInboxError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, map[string]string{"message": "deleted"})
}

// @Summary		Consultar caixa de atendimento
// @Description	Retorna os detalhes de uma caixa de atendimento a partir do seu identificador.
// @Tags			Caixas de atendimento
// @Produce		json
// @Param			id	path		string	true	"Identificador da caixa de atendimento"
// @Success		200	{object}	SupportInboxResponse
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/support/inboxes/{id} [get]
func (h *SupportInboxHandler) Get(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	id := mux.Vars(r)["id"]
	if id == "" {
		response.WriteError(w, http.StatusBadRequest, "Missing inbox id", nil)
		return
	}

	inbox, err := h.getUC.Execute(id)
	if err != nil {
		handleSupportInboxError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, toSupportInboxResponse(inbox))
}

// @Summary		Listar caixas de atendimento
// @Description	Retorna a lista paginada de caixas de atendimento do workspace, com busca por nome e filtro por arquivadas.
// @Tags			Caixas de atendimento
// @Produce		json
// @Param			page		query		int		false	"Número da página (inicia em 1)"
// @Param			pageSize	query		int		false	"Quantidade de itens por página"
// @Param			search		query		string	false	"Busca por nome da caixa de atendimento"
// @Param			archived	query		bool	false	"Filtrar por caixas arquivadas"
// @Param			sort		query		string	false	"Ordenação (ex.: name:asc, createdAt:desc)"
// @Success		200	{object}	SupportInboxListResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/support/inboxes [get]
func (h *SupportInboxHandler) List(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	query := r.URL.Query()
	options := shared.QueryOptions{
		Pagination: httpx.ParsePagination(query),
		Sorts:      parseSort(query, map[string]string{"createdat": "createdAt", "updatedat": "updatedAt", "name": "name"}),
	}

	var archived *bool
	if v := query.Get("archived"); v != "" {
		b := v == "true"
		archived = &b
	}

	result, err := h.listUC.Execute(supportinboxdomain.ListInboxesInput{
		WorkspaceID: middleware.GetWorkspaceID(r),
		Search:      query.Get("search"),
		Archived:    archived,
		Options:     options,
	})
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to list support inboxes", nil)
		return
	}

	response.WritePaginated(w, http.StatusOK, toSupportInboxResponses(result.Items), response.PaginationMeta{
		Page:       result.Page,
		PageSize:   result.PageSize,
		TotalPages: result.TotalPages,
		TotalItems: result.TotalItems,
	})
}

// @Summary		Iniciar sessão de atendimento
// @Description	Cria uma sessão de atendimento a partir do widget de chat incorporado no site do cliente. Rota pública, chamada pelo próprio widget.
// @Tags			Widget de atendimento
// @Accept			json
// @Produce		json
// @Param			body	body		CreateSessionRequest	true	"Dados de contato e caixa de atendimento"
// @Success		201	{object}	CreateSessionResponse
// @Failure		400	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Router			/public/support/sessions [post]
func (h *SupportInboxHandler) CreateSession(w http.ResponseWriter, r *http.Request) {
	var req CreateSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteInvalidBodyError(w, map[string]string{
			"inboxId":      "string (required)",
			"contactName":  "string (optional)",
			"contactEmail": "string (optional)",
			"sourceUrl":    "string (optional, the page URL where the widget is embedded)",
		})
		return
	}

	if errs := req.Validate(); errs != nil {
		response.WriteValidationError(w, errs)
		return
	}

	origin := r.Header.Get("Origin")

	result, err := h.createSessionUC.Execute(supportinboxdomain.CreateSessionInput{
		InboxID:      req.InboxID,
		ContactName:  req.ContactName,
		ContactEmail: req.ContactEmail,
		SourceURL:    req.SourceURL,
		Origin:       origin,
	})
	if err != nil {
		if err == supportinboxdomain.ErrInboxNotFound {
			response.WriteError(w, http.StatusNotFound, "Support inbox not found", nil)
			return
		}
		if strings.Contains(err.Error(), "not allowed") {
			response.WriteError(w, http.StatusForbidden, err.Error(), nil)
			return
		}
		response.WriteError(w, http.StatusInternalServerError, "Failed to create session", nil)
		return
	}

	if origin != "" {
		inbox, _ := h.getUC.Execute(req.InboxID)
		if inbox != nil && inbox.IsOriginAllowed(origin) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Vary", "Origin")
		}
	}

	response.WriteSuccess(w, http.StatusCreated, toCreateSessionResponse(result))
}

func (h *SupportInboxHandler) CreateSessionPreflight(w http.ResponseWriter, r *http.Request) {
	origin := r.Header.Get("Origin")
	inboxID := r.URL.Query().Get("inboxId")
	if inboxID == "" {

		inboxID = mux.Vars(r)["inboxId"]
	}

	if origin != "" && inboxID != "" {
		inbox, err := h.getUC.Execute(inboxID)
		if err == nil && inbox != nil && inbox.IsOriginAllowed(origin) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Access-Control-Max-Age", "86400")
			w.Header().Set("Vary", "Origin")
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// @Summary		Consultar configuração do widget
// @Description	Retorna as informações públicas de exibição do widget de chat (nome, saudação, cor e campos de pré-atendimento). Rota pública, chamada pelo próprio widget.
// @Tags			Widget de atendimento
// @Produce		json
// @Param			inboxId	path		string	true	"Identificador da caixa de atendimento"
// @Success		200	{object}	WidgetInfoResponse
// @Failure		400	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Router			/public/support/widget/{inboxId} [get]
func (h *SupportInboxHandler) GetWidgetInfo(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["inboxId"]
	if id == "" {
		response.WriteError(w, http.StatusBadRequest, "Missing inbox id", nil)
		return
	}

	inbox, err := h.getUC.Execute(id)
	if err != nil {
		if err == supportinboxdomain.ErrInboxNotFound {
			response.WriteError(w, http.StatusNotFound, "Support inbox not found", nil)
			return
		}
		response.WriteError(w, http.StatusInternalServerError, "Failed to load inbox", nil)
		return
	}

	origin := r.Header.Get("Origin")
	if origin != "" && inbox.IsOriginAllowed(origin) {
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Vary", "Origin")
	}

	response.WriteSuccess(w, http.StatusOK, toWidgetInfoResponse(inbox))
}

// @Summary		Reconectar sessão de atendimento
// @Description	Reconecta uma sessão de atendimento existente a partir do token de sessão do visitante. Rota pública, chamada pelo próprio widget.
// @Tags			Widget de atendimento
// @Accept			json
// @Produce		json
// @Param			body	body		ReconnectSessionRequest	true	"Token da sessão"
// @Success		200	{object}	ReconnectSessionResponse
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		410	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Router			/public/support/sessions/reconnect [post]
func (h *SupportInboxHandler) ReconnectSession(w http.ResponseWriter, r *http.Request) {
	var req ReconnectSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteInvalidBodyError(w, map[string]string{
			"sessionToken": "string (required)",
		})
		return
	}

	if errs := req.Validate(); errs != nil {
		response.WriteValidationError(w, errs)
		return
	}

	origin := r.Header.Get("Origin")

	result, err := h.reconnectSessionUC.Execute(supportinboxdomain.ReconnectSessionInput{
		SessionToken: req.SessionToken,
		Origin:       origin,
	})
	if err != nil {
		switch err {
		case supportinboxdomain.ErrSessionNotFound, supportinboxdomain.ErrSessionInvalid:
			response.WriteError(w, http.StatusUnauthorized, "Invalid or expired session", nil)
		case supportinboxdomain.ErrSessionExpired:
			response.WriteError(w, http.StatusGone, "Session has expired", nil)
		default:
			response.WriteError(w, http.StatusInternalServerError, "Failed to reconnect session", nil)
		}
		return
	}

	if origin != "" {
		inbox, _ := h.getUC.Execute(result.InboxID)
		if inbox != nil && inbox.IsOriginAllowed(origin) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Vary", "Origin")
		}
	}

	response.WriteSuccess(w, http.StatusOK, toReconnectSessionResponse(result))
}

func handleSupportInboxError(w http.ResponseWriter, err error) {
	switch err {
	case supportinboxdomain.ErrInboxNotFound:
		response.WriteError(w, http.StatusNotFound, "Support inbox not found", nil)
	case supportinboxdomain.ErrInboxNameRequired:
		response.WriteValidationError(w, map[string]string{"name": "required"})
	case supportinboxdomain.ErrInboxOriginInvalid:
		response.WriteError(w, http.StatusBadRequest, err.Error(), nil)
	case supportinboxdomain.ErrInboxOriginRequired:
		response.WriteError(w, http.StatusBadRequest, err.Error(), nil)
	case supportinboxdomain.ErrPreChatFieldNameEmpty:
		response.WriteError(w, http.StatusBadRequest, err.Error(), nil)
	case supportinboxdomain.ErrPreChatFieldTypeInvalid:
		response.WriteError(w, http.StatusBadRequest, err.Error(), nil)
	default:
		response.WriteError(w, http.StatusInternalServerError, "Internal server error", nil)
	}
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
