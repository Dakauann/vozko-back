package label

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/gorilla/mux"

	"vozko/delivery/http/response"
	"vozko/domain/conversation"
	ce "vozko/domain/conversation_event"
	labeldomain "vozko/domain/label"
	"vozko/infra/http/middleware"
)

type LabelHandler struct {
	createUseCase  labeldomain.CreateLabelUseCase
	updateUseCase  labeldomain.UpdateLabelUseCase
	deleteUseCase  labeldomain.DeleteLabelUseCase
	listUseCase    labeldomain.ListLabelsUseCase
	assignUseCase  labeldomain.AssignEntryLabelUseCase
	removeUseCase  labeldomain.RemoveEntryLabelUseCase
	getEntryLabels labeldomain.GetEntryLabelsUseCase
	reorderUseCase labeldomain.ReorderLabelsUseCase
	broadcaster    conversation.EventBroadcaster
	eventLogger    ce.Logger
}

func NewLabelHandler(
	createUC labeldomain.CreateLabelUseCase,
	updateUC labeldomain.UpdateLabelUseCase,
	deleteUC labeldomain.DeleteLabelUseCase,
	listUC labeldomain.ListLabelsUseCase,
	assignUC labeldomain.AssignEntryLabelUseCase,
	removeUC labeldomain.RemoveEntryLabelUseCase,
	getEntryLabelsUC labeldomain.GetEntryLabelsUseCase,
	reorderUC labeldomain.ReorderLabelsUseCase,
	broadcaster conversation.EventBroadcaster,
	eventLogger ce.Logger,
) *LabelHandler {
	return &LabelHandler{
		createUseCase:  createUC,
		updateUseCase:  updateUC,
		deleteUseCase:  deleteUC,
		listUseCase:    listUC,
		assignUseCase:  assignUC,
		removeUseCase:  removeUC,
		getEntryLabels: getEntryLabelsUC,
		reorderUseCase: reorderUC,
		broadcaster:    broadcaster,
		eventLogger:    eventLogger,
	}
}

// @Summary		Criar uma etiqueta
// @Description	Cria uma nova etiqueta no workspace para classificar conversas e chamadas. O nome é obrigatório e a cor é opcional (formato hexadecimal).
// @Tags			Etiquetas
// @Accept			json
// @Produce		json
// @Param			request	body		CreateLabelRequest	true	"Dados da etiqueta a criar"
// @Success		201	{object}	LabelResponse
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		409	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/labels [post]
func (h *LabelHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req CreateLabelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, "Invalid request body", map[string]string{
			"name":  "string (required)",
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

	created, err := h.createUseCase.Execute(wsID, labeldomain.CreateLabelInput{
		Name:  strings.TrimSpace(req.Name),
		Color: strings.TrimSpace(req.Color),
	})
	if err != nil {
		h.handleDomainError(w, err)
		return
	}

	response.WriteSuccess(w, http.StatusCreated, toLabelResponse(created))
}

// @Summary		Atualizar uma etiqueta
// @Description	Atualiza o nome e/ou a cor de uma etiqueta existente do workspace. Todos os campos são opcionais.
// @Tags			Etiquetas
// @Accept			json
// @Produce		json
// @Param			id		path		string				true	"ID da etiqueta"
// @Param			request	body		UpdateLabelRequest	true	"Campos da etiqueta a atualizar"
// @Success		200	{object}	LabelResponse
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Failure		409	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/labels/{id} [put]
func (h *LabelHandler) Update(w http.ResponseWriter, r *http.Request) {
	labelID := mux.Vars(r)["id"]

	var req UpdateLabelRequest
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

	updated, err := h.updateUseCase.Execute(wsID, labelID, labeldomain.UpdateLabelInput{
		Name:  req.Name,
		Color: req.Color,
	})
	if err != nil {
		h.handleDomainError(w, err)
		return
	}

	response.WriteSuccess(w, http.StatusOK, toLabelResponse(updated))
}

// @Summary		Remover uma etiqueta
// @Description	Exclui uma etiqueta do workspace. As associações dessa etiqueta com conversas e chamadas também são removidas.
// @Tags			Etiquetas
// @Produce		json
// @Param			id	path	string	true	"ID da etiqueta"
// @Success		204	"Etiqueta removida"
// @Failure		401	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/labels/{id} [delete]
func (h *LabelHandler) Delete(w http.ResponseWriter, r *http.Request) {
	labelID := mux.Vars(r)["id"]

	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	wsID := middleware.GetWorkspaceID(r)

	if err := h.deleteUseCase.Execute(wsID, labelID); err != nil {
		h.handleDomainError(w, err)
		return
	}

	response.WriteSuccess(w, http.StatusNoContent, nil)
}

// @Summary		Listar etiquetas
// @Description	Retorna todas as etiquetas do workspace, na ordem de exibição definida.
// @Tags			Etiquetas
// @Produce		json
// @Success		200	{array}		LabelResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/labels [get]
func (h *LabelHandler) List(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	wsID := middleware.GetWorkspaceID(r)

	labels, err := h.listUseCase.Execute(wsID)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}

	response.WriteSuccess(w, http.StatusOK, toLabelResponses(labels))
}

// @Summary		Reordenar etiquetas
// @Description	Redefine a ordem de exibição das etiquetas do workspace conforme a sequência de IDs informada.
// @Tags			Etiquetas
// @Accept			json
// @Produce		json
// @Param			request	body		ReorderLabelsRequest	true	"Lista ordenada de IDs de etiquetas"
// @Success		200	{array}		LabelResponse
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/labels/reorder [put]
func (h *LabelHandler) Reorder(w http.ResponseWriter, r *http.Request) {
	var req ReorderLabelsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, "Invalid request body", map[string]string{
			"labelIds": "[]string (required, ordered list of label IDs)",
		})
		return
	}

	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	wsID := middleware.GetWorkspaceID(r)

	labels, err := h.reorderUseCase.Execute(wsID, labeldomain.ReorderLabelsInput{
		LabelIDs: req.LabelIDs,
	})
	if err != nil {
		h.handleDomainError(w, err)
		return
	}

	response.WriteSuccess(w, http.StatusOK, toLabelResponses(labels))
}

// @Summary		Associar etiqueta a uma conversa
// @Description	Vincula uma etiqueta a uma conversa ou chamada (entryType 'voice' ou 'whatsapp'). Notifica os operadores em tempo real e registra o evento na conversa.
// @Tags			Etiquetas
// @Accept			json
// @Produce		json
// @Param			request	body		AssignEntryLabelRequest	true	"Etiqueta e conversa a associar"
// @Success		201	{object}	EntryLabelResponse
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		409	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/labels/entries [post]
func (h *LabelHandler) AssignEntryLabel(w http.ResponseWriter, r *http.Request) {
	var req AssignEntryLabelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, "Invalid request body", map[string]string{
			"labelId":   "string (required)",
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

	entryLabel, err := h.assignUseCase.Execute(wsID, labeldomain.AssignEntryLabelInput{
		LabelID:   req.LabelID,
		EntryID:   req.EntryID,
		EntryType: req.EntryType,
	})
	if err != nil {
		h.handleDomainError(w, err)
		return
	}

	if h.broadcaster != nil {
		go h.broadcaster.BroadcastLabelUpdate(wsID, req.EntryID, req.EntryType)
	}

	if h.eventLogger != nil {
		h.eventLogger.Log(&ce.ConversationEvent{
			WorkspaceID: wsID,
			EntryID:     req.EntryID,
			EntryType:   req.EntryType,
			EventType:   ce.EventLabelAdded,
			ActorID:     claims.UserID,
			Details:     ce.DetailsJSON(map[string]string{"label_id": req.LabelID}),
		})
	}

	response.WriteSuccess(w, http.StatusCreated, toEntryLabelResponse(entryLabel))
}

// @Summary		Remover etiqueta de uma conversa
// @Description	Desvincula uma etiqueta de uma conversa ou chamada. Notifica os operadores em tempo real e registra o evento na conversa.
// @Tags			Etiquetas
// @Accept			json
// @Produce		json
// @Param			request	body		RemoveEntryLabelRequest	true	"Etiqueta e conversa a desassociar"
// @Success		200	{object}	MessageResponse
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/labels/entries [delete]
func (h *LabelHandler) RemoveEntryLabel(w http.ResponseWriter, r *http.Request) {
	var req RemoveEntryLabelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, "Invalid request body", map[string]string{
			"labelId":   "string (required)",
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

	if err := h.removeUseCase.Execute(wsID, req.LabelID, req.EntryID, req.EntryType); err != nil {
		h.handleDomainError(w, err)
		return
	}

	if h.broadcaster != nil {
		go h.broadcaster.BroadcastLabelUpdate(wsID, req.EntryID, req.EntryType)
	}

	if h.eventLogger != nil {
		h.eventLogger.Log(&ce.ConversationEvent{
			WorkspaceID: wsID,
			EntryID:     req.EntryID,
			EntryType:   req.EntryType,
			EventType:   ce.EventLabelRemoved,
			ActorID:     claims.UserID,
			Details:     ce.DetailsJSON(map[string]string{"label_id": req.LabelID}),
		})
	}

	response.WriteSuccess(w, http.StatusOK, map[string]string{"message": "label removed"})
}

// @Summary		Listar etiquetas de uma conversa
// @Description	Retorna as etiquetas associadas a uma conversa ou chamada específica, identificada pelo tipo e ID da entrada.
// @Tags			Etiquetas
// @Produce		json
// @Param			entryType	path		string	true	"Tipo da entrada ('voice' ou 'whatsapp')"
// @Param			entryId		path		string	true	"ID da entrada"
// @Success		200	{array}		EntryLabelResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/labels/entries/{entryType}/{entryId} [get]
func (h *LabelHandler) GetEntryLabels(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	entryID := vars["entryId"]
	entryType := vars["entryType"]

	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	wsID := middleware.GetWorkspaceID(r)

	labels, err := h.getEntryLabels.Execute(wsID, entryID, entryType)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}

	response.WriteSuccess(w, http.StatusOK, toEntryLabelResponses(labels))
}

func (h *LabelHandler) handleDomainError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, labeldomain.ErrLabelNotFound):
		response.WriteError(w, http.StatusNotFound, err.Error(), nil)
	case errors.Is(err, labeldomain.ErrLabelNameRequired):
		response.WriteError(w, http.StatusBadRequest, err.Error(), nil)
	case errors.Is(err, labeldomain.ErrLabelNameExists):
		response.WriteError(w, http.StatusConflict, err.Error(), nil)
	case errors.Is(err, labeldomain.ErrEntryLabelExists):
		response.WriteError(w, http.StatusConflict, err.Error(), nil)
	case errors.Is(err, labeldomain.ErrEntryLabelNotFound):
		response.WriteError(w, http.StatusNotFound, err.Error(), nil)
	case errors.Is(err, labeldomain.ErrInvalidEntryType):
		response.WriteError(w, http.StatusBadRequest, err.Error(), nil)
	case errors.Is(err, labeldomain.ErrUnauthorized):
		response.WriteError(w, http.StatusForbidden, err.Error(), nil)
	default:
		response.WriteError(w, http.StatusInternalServerError, "Internal server error", nil)
	}
}
