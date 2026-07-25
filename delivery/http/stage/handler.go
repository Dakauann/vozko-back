package stage

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/gorilla/mux"

	"vozko/delivery/http/response"
	"vozko/domain/conversation"
	ce "vozko/domain/conversation_event"
	stagedomain "vozko/domain/stage"
	"vozko/infra/http/middleware"
)

type StageHandler struct {
	createUseCase       stagedomain.CreateStageUseCase
	updateUseCase       stagedomain.UpdateStageUseCase
	deleteUseCase       stagedomain.DeleteStageUseCase
	listUseCase         stagedomain.ListStagesUseCase
	setInitialUseCase   stagedomain.SetInitialStageUseCase
	assignUseCase       stagedomain.AssignEntryStageUseCase
	removeUseCase       stagedomain.RemoveEntryStageUseCase
	getEntryTagUC       stagedomain.GetEntryStageUseCase
	getBatchEntryTagsUC stagedomain.GetBatchEntryStagesUseCase
	reorderUseCase      stagedomain.ReorderStagesUseCase
	broadcaster         conversation.EventBroadcaster
	eventLogger         ce.Logger
}

func NewStageHandler(
	createUC stagedomain.CreateStageUseCase,
	updateUC stagedomain.UpdateStageUseCase,
	deleteUC stagedomain.DeleteStageUseCase,
	listUC stagedomain.ListStagesUseCase,
	setInitialUC stagedomain.SetInitialStageUseCase,
	assignUC stagedomain.AssignEntryStageUseCase,
	removeUC stagedomain.RemoveEntryStageUseCase,
	getEntryTagUC stagedomain.GetEntryStageUseCase,
	getBatchEntryTagsUC stagedomain.GetBatchEntryStagesUseCase,
	reorderUC stagedomain.ReorderStagesUseCase,
	broadcaster conversation.EventBroadcaster,
	eventLogger ce.Logger,
) *StageHandler {
	return &StageHandler{
		createUseCase:       createUC,
		updateUseCase:       updateUC,
		deleteUseCase:       deleteUC,
		listUseCase:         listUC,
		setInitialUseCase:   setInitialUC,
		assignUseCase:       assignUC,
		removeUseCase:       removeUC,
		getEntryTagUC:       getEntryTagUC,
		getBatchEntryTagsUC: getBatchEntryTagsUC,
		reorderUseCase:      reorderUC,
		broadcaster:         broadcaster,
		eventLogger:         eventLogger,
	}
}

// @Summary		Criar etapa
// @Description	Cria uma nova etapa (coluna do funil) no pipeline de conversas do workspace. O nome é obrigatório e a cor é opcional (formato hexadecimal).
// @Tags			Etapas
// @Accept			json
// @Produce		json
// @Param			request	body		CreateTagRequest	true	"Dados da etapa a criar"
// @Success		201	{object}	stage.Stage
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		409	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/stages [post]
func (h *StageHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req CreateTagRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, "Invalid request body", map[string]string{
			"name":        "string (required)",
			"description": "string (required)",
			"color":       "string (optional, hex color)",
		})
		return
	}

	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	wsID := middleware.GetWorkspaceID(r)

	created, err := h.createUseCase.Execute(wsID, stagedomain.CreateStageInput{
		Name:         strings.TrimSpace(req.Name),
		Description:  strings.TrimSpace(req.Description),
		Color:        strings.TrimSpace(req.Color),
		CampaignID:   strings.TrimSpace(req.CampaignID),
		CampaignType: strings.TrimSpace(req.CampaignType),
	})
	if err != nil {
		h.handleDomainError(w, err)
		return
	}

	response.WriteSuccess(w, http.StatusCreated, created)
}

// @Summary		Atualizar etapa
// @Description	Atualiza o nome, a descrição e/ou a cor de uma etapa existente. Todos os campos são opcionais.
// @Tags			Etapas
// @Accept			json
// @Produce		json
// @Param			id		path		string				true	"ID da etapa"
// @Param			request	body		UpdateTagRequest	true	"Campos da etapa a atualizar"
// @Success		200	{object}	stage.Stage
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/stages/{id} [put]
func (h *StageHandler) Update(w http.ResponseWriter, r *http.Request) {
	StageID := mux.Vars(r)["id"]

	var req UpdateTagRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteInvalidBodyError(w, map[string]string{
			"name":  "string (optional)",
			"color": "string (optional, hex color)",
		})
		return
	}

	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	wsID := middleware.GetWorkspaceID(r)

	updated, err := h.updateUseCase.Execute(wsID, StageID, stagedomain.UpdateStageInput{
		Name:        req.Name,
		Description: req.Description,
		Color:       req.Color,
	})
	if err != nil {
		h.handleDomainError(w, err)
		return
	}

	response.WriteSuccess(w, http.StatusOK, updated)
}

// @Summary		Remover etapa
// @Description	Exclui uma etapa do pipeline do workspace. Etapas padrão não podem ser removidas.
// @Tags			Etapas
// @Produce		json
// @Param			id	path	string	true	"ID da etapa"
// @Success		204	"Etapa removida"
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/stages/{id} [delete]
func (h *StageHandler) Delete(w http.ResponseWriter, r *http.Request) {
	StageID := mux.Vars(r)["id"]

	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	wsID := middleware.GetWorkspaceID(r)

	if err := h.deleteUseCase.Execute(wsID, StageID); err != nil {
		h.handleDomainError(w, err)
		return
	}

	response.WriteSuccess(w, http.StatusNoContent, nil)
}

// @Summary		Listar etapas
// @Description	Retorna as etapas do pipeline de conversas do workspace, na ordem de exibição. Os parâmetros campaignId e campaignType são aceitos por compatibilidade, mas não são obrigatórios.
// @Tags			Etapas
// @Produce		json
// @Param			campaignId		query	string	false	"ID da campanha (compatibilidade)"
// @Param			campaignType	query	string	false	"Tipo da campanha (compatibilidade)"
// @Success		200	{array}		stage.Stage
// @Failure		401	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/stages [get]
func (h *StageHandler) List(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	wsID := middleware.GetWorkspaceID(r)
	campaignID := r.URL.Query().Get("campaignId")
	campaignType := r.URL.Query().Get("campaignType")

	tags, err := h.listUseCase.Execute(wsID, campaignID, campaignType)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}

	response.WriteSuccess(w, http.StatusOK, tags)
}

// @Summary		Definir etapa inicial
// @Description	Define a etapa informada como a etapa inicial do pipeline do workspace.
// @Tags			Etapas
// @Accept			json
// @Produce		json
// @Param			request	body		SetInitialTagRequest	true	"ID da etapa a marcar como inicial"
// @Success		200	{object}	stage.Stage
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/stages/initial [put]
func (h *StageHandler) SetInitialStage(w http.ResponseWriter, r *http.Request) {
	var req SetInitialTagRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, "Invalid request body", map[string]string{
			"StageID": "string (required)",
		})
		return
	}

	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	wsID := middleware.GetWorkspaceID(r)

	updated, err := h.setInitialUseCase.Execute(wsID, req.StageID)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}

	response.WriteSuccess(w, http.StatusOK, updated)
}

// @Summary		Reordenar etapas
// @Description	Redefine a ordem de exibição das etapas do pipeline conforme a sequência de IDs informada.
// @Tags			Etapas
// @Accept			json
// @Produce		json
// @Param			request	body		ReorderTagsRequest	true	"Lista ordenada de IDs de etapas"
// @Success		200	{array}		stage.Stage
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/stages/reorder [put]
func (h *StageHandler) Reorder(w http.ResponseWriter, r *http.Request) {
	var req ReorderTagsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, "Invalid request body", map[string]string{
			"tagIds": "[]string (required, ordered list of tag IDs)",
		})
		return
	}

	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	wsID := middleware.GetWorkspaceID(r)

	tags, err := h.reorderUseCase.Execute(wsID, stagedomain.ReorderStagesInput{
		StageIDs:     req.StageIDs,
		CampaignID:   req.CampaignID,
		CampaignType: req.CampaignType,
	})
	if err != nil {
		h.handleDomainError(w, err)
		return
	}

	response.WriteSuccess(w, http.StatusOK, tags)
}

// @Summary		Associar etapa a uma conversa
// @Description	Move uma conversa ou chamada (entryType 'voice' ou 'whatsapp') para uma etapa. Notifica os operadores em tempo real e registra o evento na conversa.
// @Tags			Etapas
// @Accept			json
// @Produce		json
// @Param			request	body		AssignEntryTagRequest	true	"Etapa e conversa a associar"
// @Success		201	{object}	stage.EntryStage
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		409	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/stages/entries [post]
func (h *StageHandler) AssignEntryStage(w http.ResponseWriter, r *http.Request) {
	var req AssignEntryTagRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, "Invalid request body", map[string]string{
			"StageID":   "string (required)",
			"entryId":   "string (required)",
			"entryType": "string (required, 'voice' or 'whatsapp')",
		})
		return
	}

	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	wsID := middleware.GetWorkspaceID(r)

	EntryStage, err := h.assignUseCase.Execute(wsID, stagedomain.AssignEntryStageInput{
		StageID:   req.StageID,
		EntryID:   req.EntryID,
		EntryType: req.EntryType,
	})
	if err != nil {
		h.handleDomainError(w, err)
		return
	}

	if h.broadcaster != nil {
		go h.broadcaster.BroadcastStageUpdate(wsID, req.EntryID, req.EntryType)
	}

	if h.eventLogger != nil {
		h.eventLogger.Log(ce.New(wsID, req.EntryID, req.EntryType, ce.EventStageChanged).
			WithActorHuman(claims.UserID).
			WithDetails(map[string]string{"stage_id": req.StageID, "to_stage_id": req.StageID}).
			Build())
		h.eventLogger.Log(ce.New(wsID, req.EntryID, req.EntryType, ce.EventTagAdded).
			WithActorHuman(claims.UserID).
			WithDetails(map[string]string{"stage_id": req.StageID}).
			Build())
	}

	response.WriteSuccess(w, http.StatusCreated, EntryStage)
}

// @Summary		Remover etapa de uma conversa
// @Description	Desassocia uma etapa de uma conversa ou chamada. Notifica os operadores em tempo real e registra o evento na conversa.
// @Tags			Etapas
// @Accept			json
// @Produce		json
// @Param			request	body		RemoveEntryTagRequest	true	"Etapa e conversa a desassociar"
// @Success		200	{object}	MessageResponse
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/stages/entries [delete]
func (h *StageHandler) RemoveEntryStage(w http.ResponseWriter, r *http.Request) {
	var req RemoveEntryTagRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, "Invalid request body", map[string]string{
			"StageID":   "string (required)",
			"entryId":   "string (required)",
			"entryType": "string (required, 'voice' or 'whatsapp')",
		})
		return
	}

	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	wsID := middleware.GetWorkspaceID(r)

	if err := h.removeUseCase.Execute(wsID, req.StageID, req.EntryID, req.EntryType); err != nil {
		h.handleDomainError(w, err)
		return
	}

	if h.broadcaster != nil {
		go h.broadcaster.BroadcastStageUpdate(wsID, req.EntryID, req.EntryType)
	}

	if h.eventLogger != nil {
		h.eventLogger.Log(&ce.ConversationEvent{
			WorkspaceID: wsID,
			EntryID:     req.EntryID,
			EntryType:   req.EntryType,
			EventType:   ce.EventTagRemoved,
			ActorID:     claims.UserID,
			Details:     ce.DetailsJSON(map[string]string{"stage_id": req.StageID}),
		})
	}

	response.WriteSuccess(w, http.StatusOK, map[string]string{"message": "tag removed"})
}

// @Summary		Obter etapa de uma conversa
// @Description	Retorna a etapa atual de uma conversa ou chamada específica, identificada pelo tipo e ID da entrada.
// @Tags			Etapas
// @Produce		json
// @Param			entryType	path		string	true	"Tipo da entrada ('voice' ou 'whatsapp')"
// @Param			entryId		path		string	true	"ID da entrada"
// @Success		200	{object}	stage.EntryStage
// @Failure		401	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/stages/entries/{entryType}/{entryId} [get]
func (h *StageHandler) GetEntryStage(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	entryID := vars["entryId"]
	entryType := vars["entryType"]

	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	wsID := middleware.GetWorkspaceID(r)

	EntryStage, err := h.getEntryTagUC.Execute(wsID, entryID, entryType)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}

	response.WriteSuccess(w, http.StatusOK, EntryStage)
}

// @Summary		Obter etapas de várias conversas
// @Description	Retorna, em lote, a etapa atual de várias conversas ou chamadas do mesmo tipo. Aceita no máximo 100 entradas por requisição.
// @Tags			Etapas
// @Accept			json
// @Produce		json
// @Param			request	body		object	true	"Lista de IDs de entrada e o tipo"
// @Success		200	{object}	map[string]stage.EntryStage
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/stages/entries/batch [post]
func (h *StageHandler) GetBatchEntryStages(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	wsID := middleware.GetWorkspaceID(r)

	var req struct {
		EntryIDs  []string `json:"entryIds"`
		EntryType string   `json:"entryType"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, "Invalid request body", nil)
		return
	}

	if len(req.EntryIDs) == 0 || strings.TrimSpace(req.EntryType) == "" {
		response.WriteError(w, http.StatusBadRequest, "entryIds and entryType are required", nil)
		return
	}

	if len(req.EntryIDs) > 100 {
		req.EntryIDs = req.EntryIDs[:100]
	}

	result, err := h.getBatchEntryTagsUC.Execute(wsID, req.EntryIDs, req.EntryType)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}

	response.WriteSuccess(w, http.StatusOK, result)
}

func (h *StageHandler) handleDomainError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, stagedomain.ErrTagNotFound):
		response.WriteError(w, http.StatusNotFound, err.Error(), nil)
	case errors.Is(err, stagedomain.ErrTagNameRequired):
		response.WriteError(w, http.StatusBadRequest, err.Error(), nil)
	case errors.Is(err, stagedomain.ErrTagDescRequired):
		response.WriteError(w, http.StatusBadRequest, err.Error(), nil)
	case errors.Is(err, stagedomain.ErrTagNameExists):
		response.WriteError(w, http.StatusConflict, err.Error(), nil)
	case errors.Is(err, stagedomain.ErrTagDefaultDelete):
		response.WriteError(w, http.StatusForbidden, err.Error(), nil)
	case errors.Is(err, stagedomain.ErrTagDefaultUpdate):
		response.WriteError(w, http.StatusForbidden, err.Error(), nil)
	case errors.Is(err, stagedomain.ErrEntryTagExists):
		response.WriteError(w, http.StatusConflict, err.Error(), nil)
	case errors.Is(err, stagedomain.ErrEntryTagNotFound):
		response.WriteError(w, http.StatusNotFound, err.Error(), nil)
	case errors.Is(err, stagedomain.ErrInvalidEntryType):
		response.WriteError(w, http.StatusBadRequest, err.Error(), nil)
	case errors.Is(err, stagedomain.ErrUnauthorized):
		response.WriteError(w, http.StatusForbidden, err.Error(), nil)
	default:
		response.WriteError(w, http.StatusInternalServerError, "Internal server error", nil)
	}
}
