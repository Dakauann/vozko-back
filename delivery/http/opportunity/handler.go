package opportunity

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/gorilla/mux"

	"vozko/delivery/http/response"
	"vozko/domain/conversation"
	"vozko/domain/customfield"
	opportunitydomain "vozko/domain/opportunity"
	"vozko/infra/http/middleware"
	opportunity_usecase "vozko/usecases/opportunity"
	"vozko/usecases/opportunityio"
	report_usecase "vozko/usecases/report"
)

type opportunityScoper interface {
	GetDepartmentScope(userID, workspaceID string, isAdmin bool) (conversation.DepartmentAccessScope, bool)
}

type OpportunityHandler struct {
	svc     *opportunity_usecase.Service
	io      *opportunityio.Service
	scoper  opportunityScoper
	reports *report_usecase.Service
}

func NewOpportunityHandler(
	svc *opportunity_usecase.Service,
	io *opportunityio.Service,
	scoper opportunityScoper,
	reports *report_usecase.Service,
) *OpportunityHandler {
	return &OpportunityHandler{svc: svc, io: io, scoper: scoper, reports: reports}
}

func (h *OpportunityHandler) resolveScope(userID, workspaceID string, isAdmin bool) (deptIDs []string, restrict bool, assigneeOverride string, allowed bool) {
	if h.scoper == nil {
		return nil, false, "", true
	}
	scope, ok := h.scoper.GetDepartmentScope(userID, workspaceID, isAdmin)
	if !ok {
		return nil, false, "", false
	}
	deptIDs = scope.DepartmentIDs
	restrict = scope.Restrict
	if !isAdmin && restrict {
		assigneeOverride = userID
	}
	return deptIDs, restrict, assigneeOverride, true
}

// @Summary		Criar oportunidade
// @Description	Cria uma nova oportunidade (negócio) no funil de vendas do workspace. O pipeline e a etapa são obrigatórios; o título é obrigatório quando não há um lead associado.
// @Tags			Oportunidades
// @Accept			json
// @Produce		json
// @Param			request	body		CreateOpportunityRequest	true	"Dados da oportunidade a criar"
// @Success		201	{object}	opportunity.Opportunity
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/opportunities [post]
func (h *OpportunityHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req CreateOpportunityRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, "Invalid request body", map[string]string{
			"pipelineId": "string (required)",
			"stageId":    "string (required)",
			"title":      "string (required unless leadId is set)",
		})
		return
	}
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}
	wsID := middleware.GetWorkspaceID(r)

	created, err := h.svc.Create(wsID, opportunity_usecase.CreateInput{
		LeadID:        strings.TrimSpace(req.LeadID),
		PipelineID:    strings.TrimSpace(req.PipelineID),
		StageID:       strings.TrimSpace(req.StageID),
		OwnerID:       strings.TrimSpace(req.OwnerID),
		CarteiraID:    strings.TrimSpace(req.CarteiraID),
		Title:         req.Title,
		ValueCents:    req.ValueCents,
		Currency:      req.Currency,
		Source:        strings.TrimSpace(req.Source),
		CloseDate:     req.CloseDate,
		CustomFields:  req.CustomFields,
		LinkEntryID:   strings.TrimSpace(req.LinkEntryID),
		LinkEntryType: strings.TrimSpace(req.LinkEntryType),
		Actor:         claims.UserID,
	})
	if err != nil {
		h.handleDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusCreated, created)
}

// @Summary		Atualizar oportunidade
// @Description	Atualiza os campos de uma oportunidade existente. Todos os campos são opcionais e apenas os informados são alterados.
// @Tags			Oportunidades
// @Accept			json
// @Produce		json
// @Param			id		path		string						true	"ID da oportunidade"
// @Param			request	body		UpdateOpportunityRequest	true	"Campos da oportunidade a atualizar"
// @Success		200	{object}	opportunity.Opportunity
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/opportunities/{id} [patch]
func (h *OpportunityHandler) Update(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]

	var req UpdateOpportunityRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteInvalidBodyError(w, map[string]string{
			"title": "string (optional)",
			"value": "number (optional, minor units)",
		})
		return
	}
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}
	wsID := middleware.GetWorkspaceID(r)

	updated, err := h.svc.Update(wsID, id, opportunity_usecase.UpdateInput{
		Title:        req.Title,
		ValueCents:   req.ValueCents,
		Currency:     req.Currency,
		OwnerID:      req.OwnerID,
		CarteiraID:   req.CarteiraID,
		Source:       req.Source,
		CustomFields: req.CustomFields,
		StageID:      req.StageID,
		LostReasonID: req.LostReasonID,
	}, claims.UserID)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, updated)
}

// @Summary		Mover oportunidade de etapa
// @Description	Move uma oportunidade para outra etapa do funil. O status vem da etapa: uma etapa de ganho marca como ganha (e exige valor), uma de perda marca como perdida (e exige o motivo), qualquer outra reabre.
// @Tags			Oportunidades
// @Accept			json
// @Produce		json
// @Param			id		path		string				true	"ID da oportunidade"
// @Param			request	body		MoveStageRequest	true	"Etapa de destino"
// @Success		200	{object}	opportunity.Opportunity
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/opportunities/{id}/move [post]
func (h *OpportunityHandler) MoveStage(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]

	var req MoveStageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, "Invalid request body", map[string]string{
			"stageId": "string (required)",
		})
		return
	}
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}
	wsID := middleware.GetWorkspaceID(r)

	moved, err := h.svc.MoveStage(wsID, id, opportunity_usecase.MoveStageInput{
		StageID:      strings.TrimSpace(req.StageID),
		LostReasonID: strings.TrimSpace(req.LostReasonID),
	}, claims.UserID)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, moved)
}

// @Summary		Obter oportunidade
// @Description	Retorna os detalhes de uma oportunidade específica do workspace.
// @Tags			Oportunidades
// @Produce		json
// @Param			id	path		string	true	"ID da oportunidade"
// @Success		200	{object}	opportunity.Opportunity
// @Failure		401	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/opportunities/{id} [get]
func (h *OpportunityHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]

	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}
	wsID := middleware.GetWorkspaceID(r)

	o, err := h.svc.Get(wsID, id)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, o)
}

// @Summary		Listar oportunidades do pipeline
// @Description	Retorna as oportunidades de um pipeline do workspace, respeitando o escopo de departamento do usuário. O parâmetro pipelineId é obrigatório.
// @Tags			Oportunidades
// @Produce		json
// @Param			pipelineId	query	string	true	"ID do pipeline"
// @Success		200	{array}		opportunity.Opportunity
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/opportunities [get]
func (h *OpportunityHandler) ListByPipeline(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}
	wsID := middleware.GetWorkspaceID(r)
	pipelineID := strings.TrimSpace(r.URL.Query().Get("pipelineId"))
	if pipelineID == "" {
		response.WriteError(w, http.StatusBadRequest, "pipelineId is required", nil)
		return
	}

	deptIDs, restrict, override, allowed := h.resolveScope(claims.UserID, wsID, claims.Role == "admin")
	if !allowed {
		response.WriteError(w, http.StatusForbidden, "Forbidden", nil)
		return
	}
	list, err := h.svc.ListByPipelineScoped(wsID, pipelineID, deptIDs, restrict, override)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, list)
}

// @Summary		Remover oportunidade
// @Description	Exclui uma oportunidade do funil de vendas do workspace.
// @Tags			Oportunidades
// @Produce		json
// @Param			id	path	string	true	"ID da oportunidade"
// @Success		204	"Oportunidade removida"
// @Failure		401	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/opportunities/{id} [delete]
func (h *OpportunityHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]

	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}
	wsID := middleware.GetWorkspaceID(r)

	if err := h.svc.Delete(wsID, id); err != nil {
		h.handleDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusNoContent, nil)
}

// @Summary		Vincular conversa à oportunidade
// @Description	Associa uma conversa (entryType 'whatsapp' ou 'support') a uma oportunidade.
// @Tags			Oportunidades
// @Accept			json
// @Produce		json
// @Param			id		path		string					true	"ID da oportunidade"
// @Param			request	body		LinkConversationRequest	true	"Conversa a vincular"
// @Success		201	{object}	map[string]string
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/opportunities/{id}/conversations [post]
func (h *OpportunityHandler) LinkConversation(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]

	var req LinkConversationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, "Invalid request body", map[string]string{
			"entryId":   "string (required)",
			"entryType": "string (required, 'whatsapp' | 'support')",
		})
		return
	}
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}
	wsID := middleware.GetWorkspaceID(r)

	if err := h.svc.LinkConversation(wsID, id, strings.TrimSpace(req.EntryID), strings.TrimSpace(req.EntryType), claims.UserID); err != nil {
		h.handleDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusCreated, map[string]string{"message": "conversation linked"})
}

// @Summary		Desvincular conversa da oportunidade
// @Description	Remove a associação entre uma conversa ou chamada e uma oportunidade.
// @Tags			Oportunidades
// @Accept			json
// @Produce		json
// @Param			id		path		string					true	"ID da oportunidade"
// @Param			request	body		LinkConversationRequest	true	"Conversa a desvincular"
// @Success		200	{object}	map[string]string
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/opportunities/{id}/conversations [delete]
func (h *OpportunityHandler) UnlinkConversation(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]

	var req LinkConversationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, "Invalid request body", map[string]string{
			"entryId":   "string (required)",
			"entryType": "string (required)",
		})
		return
	}
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}
	wsID := middleware.GetWorkspaceID(r)

	if err := h.svc.UnlinkConversation(wsID, id, strings.TrimSpace(req.EntryID), strings.TrimSpace(req.EntryType)); err != nil {
		h.handleDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, map[string]string{"message": "conversation unlinked"})
}

// @Summary		Listar conversas da oportunidade
// @Description	Retorna as conversas e chamadas vinculadas a uma oportunidade.
// @Tags			Oportunidades
// @Produce		json
// @Param			id	path		string	true	"ID da oportunidade"
// @Success		200	{array}		opportunity.ConversationLink
// @Failure		401	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/opportunities/{id}/conversations [get]
func (h *OpportunityHandler) ListConversations(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]

	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}
	wsID := middleware.GetWorkspaceID(r)

	links, err := h.svc.ListConversations(wsID, id)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, links)
}

// @Summary		Listar oportunidades de uma conversa
// @Description	Retorna as oportunidades vinculadas a uma conversa ou chamada, identificada por entryId e entryType, para exibição no painel lateral da conversa.
// @Tags			Oportunidades
// @Produce		json
// @Param			entryId		query	string	true	"ID da entrada (conversa ou chamada)"
// @Param			entryType	query	string	true	"Tipo da entrada ('whatsapp' ou 'support')"
// @Success		200	{array}		opportunity.Opportunity
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/opportunities/for-entry [get]
func (h *OpportunityHandler) ListForEntry(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}
	wsID := middleware.GetWorkspaceID(r)
	q := r.URL.Query()
	entryID := q.Get("entryId")
	entryType := q.Get("entryType")
	if entryID == "" || entryType == "" {
		response.WriteError(w, http.StatusBadRequest, "entryId and entryType are required", nil)
		return
	}
	opps, err := h.svc.ListOpportunitiesForEntry(wsID, entryID, entryType)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, opps)
}

func (h *OpportunityHandler) handleDomainError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, opportunitydomain.ErrNotFound):
		response.WriteError(w, http.StatusNotFound, err.Error(), nil)
	case errors.Is(err, opportunitydomain.ErrWorkspaceRequired),
		errors.Is(err, opportunitydomain.ErrPipelineRequired),
		errors.Is(err, opportunitydomain.ErrStageRequired),
		errors.Is(err, opportunitydomain.ErrTitleOrLead),
		errors.Is(err, opportunitydomain.ErrInvalidStatus),
		errors.Is(err, opportunitydomain.ErrNegativeValue),
		errors.Is(err, opportunitydomain.ErrLostReasonMissing),
		errors.Is(err, opportunitydomain.ErrWonWithoutValue),
		errors.Is(err, opportunitydomain.ErrUnsupportedCurrency),
		errors.Is(err, opportunitydomain.ErrStageOutsidePipeline),
		errors.Is(err, opportunitydomain.ErrInvalidAmount),
		errors.Is(err, opportunity_usecase.ErrOwnerOutsideWorkspace),
		errors.Is(err, opportunity_usecase.ErrPipelineNotFound),
		errors.Is(err, opportunity_usecase.ErrNotOpportunityPipeline),
		errors.Is(err, opportunity_usecase.ErrStageNotFound),
		errors.Is(err, opportunity_usecase.ErrUnknownCustomField),
		errors.Is(err, opportunity_usecase.ErrEntryTypeRequired),
		errors.Is(err, customfield.ErrValueType),
		errors.Is(err, customfield.ErrValueNotInOptions),
		errors.Is(err, customfield.ErrValueRequired):
		response.WriteError(w, http.StatusBadRequest, err.Error(), nil)
	default:
		response.WriteError(w, http.StatusInternalServerError, "Internal server error", nil)
	}
}

// @Summary		Histórico da oportunidade
// @Description	Lista, em ordem cronológica, cada mudança da oportunidade (criação, etapa, ganho, perda, reabertura, valor, responsável e vínculo com conversa) com quem a fez: uma pessoa, um agente de IA (ai:<id>) ou um fluxo (workflow:<id>).
// @Tags			Oportunidades
// @Produce		json
// @Param			id	path		string	true	"ID da oportunidade"
// @Success		200	{array}		opportunity.Event
// @Failure		401	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/opportunities/{id}/events [get]
func (h *OpportunityHandler) ListEvents(w http.ResponseWriter, r *http.Request) {
	if middleware.GetClaims(r) == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}
	events, err := h.svc.ListEvents(middleware.GetWorkspaceID(r), mux.Vars(r)["id"])
	if err != nil {
		h.handleDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, events)
}
