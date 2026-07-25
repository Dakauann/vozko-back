package messageshortcut

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/gorilla/mux"

	"vozko/delivery/http/response"
	messageshortcutdomain "vozko/domain/message_shortcut"
	"vozko/infra/http/middleware"
)

type MessageShortcutHandler struct {
	createUC        messageshortcutdomain.CreateUseCase
	updateUC        messageshortcutdomain.UpdateUseCase
	deleteUC        messageshortcutdomain.DeleteUseCase
	listUC          messageshortcutdomain.ListUseCase
	getByShortcutUC messageshortcutdomain.GetByShortcutUseCase
}

func NewMessageShortcutHandler(
	createUC messageshortcutdomain.CreateUseCase,
	updateUC messageshortcutdomain.UpdateUseCase,
	deleteUC messageshortcutdomain.DeleteUseCase,
	listUC messageshortcutdomain.ListUseCase,
	getByShortcutUC messageshortcutdomain.GetByShortcutUseCase,
) *MessageShortcutHandler {
	return &MessageShortcutHandler{
		createUC:        createUC,
		updateUC:        updateUC,
		deleteUC:        deleteUC,
		listUC:          listUC,
		getByShortcutUC: getByShortcutUC,
	}
}

// @Summary		Criar atalho de mensagem
// @Description	Cria um atalho de mensagem reutilizável no workspace. O nome, o atalho (sem espaços), o tipo e o conteúdo são obrigatórios.
// @Tags			Atalhos de Mensagem
// @Accept			json
// @Produce		json
// @Param			request	body		CreateShortcutRequest	true	"Dados do atalho a criar"
// @Success		201	{object}	message_shortcut.MessageShortcut
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		409	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/message-shortcuts [post]
func (h *MessageShortcutHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req CreateShortcutRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteInvalidBodyError(w, map[string]string{
			"name":        "string (required)",
			"shortcut":    "string (required, no spaces)",
			"messageType": "text | button | media",
			"content":     "object (required)",
		})
		return
	}

	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	wsID := middleware.GetWorkspaceID(r)

	created, err := h.createUC.Execute(wsID, messageshortcutdomain.CreateInput{
		Name:        req.Name,
		Shortcut:    req.Shortcut,
		MessageType: messageshortcutdomain.MessageType(req.MessageType),
		Content:     req.Content.toDomain(),
	})
	if err != nil {
		h.handleDomainError(w, err)
		return
	}

	response.WriteSuccess(w, http.StatusCreated, created)
}

// @Summary		Atualizar atalho de mensagem
// @Description	Atualiza o nome, o atalho, o tipo e/ou o conteúdo de um atalho de mensagem existente. Todos os campos são opcionais.
// @Tags			Atalhos de Mensagem
// @Accept			json
// @Produce		json
// @Param			id		path		string					true	"ID do atalho"
// @Param			request	body		UpdateShortcutRequest	true	"Campos a atualizar"
// @Success		200	{object}	message_shortcut.MessageShortcut
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/message-shortcuts/{id} [put]
func (h *MessageShortcutHandler) Update(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]

	var req UpdateShortcutRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteInvalidBodyError(w, map[string]string{
			"name":        "string (optional)",
			"shortcut":    "string (optional)",
			"messageType": "text | button | media (optional)",
			"content":     "object (optional)",
		})
		return
	}

	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	wsID := middleware.GetWorkspaceID(r)

	updated, err := h.updateUC.Execute(wsID, id, messageshortcutdomain.UpdateInput{
		Name:        req.Name,
		Shortcut:    req.Shortcut,
		MessageType: toMessageType(req.MessageType),
		Content:     toShortcutContent(req.Content),
	})
	if err != nil {
		h.handleDomainError(w, err)
		return
	}

	response.WriteSuccess(w, http.StatusOK, updated)
}

// @Summary		Remover atalho de mensagem
// @Description	Exclui um atalho de mensagem do workspace.
// @Tags			Atalhos de Mensagem
// @Produce		json
// @Param			id	path	string	true	"ID do atalho"
// @Success		204	"Atalho removido"
// @Failure		401	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/message-shortcuts/{id} [delete]
func (h *MessageShortcutHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]

	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	wsID := middleware.GetWorkspaceID(r)

	if err := h.deleteUC.Execute(wsID, id); err != nil {
		h.handleDomainError(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// @Summary		Listar atalhos de mensagem
// @Description	Retorna todos os atalhos de mensagem do workspace.
// @Tags			Atalhos de Mensagem
// @Produce		json
// @Success		200	{array}		message_shortcut.MessageShortcut
// @Failure		401	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/message-shortcuts [get]
func (h *MessageShortcutHandler) List(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	wsID := middleware.GetWorkspaceID(r)

	shortcuts, err := h.listUC.Execute(wsID)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to list shortcuts", nil)
		return
	}

	response.WriteSuccess(w, http.StatusOK, shortcuts)
}

// @Summary		Obter atalho pelo gatilho
// @Description	Retorna um atalho de mensagem do workspace pelo seu gatilho (por exemplo '/ola').
// @Tags			Atalhos de Mensagem
// @Produce		json
// @Param			shortcut	path		string	true	"Gatilho do atalho"
// @Success		200	{object}	message_shortcut.MessageShortcut
// @Failure		401	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/message-shortcuts/by-shortcut/{shortcut} [get]
func (h *MessageShortcutHandler) GetByShortcut(w http.ResponseWriter, r *http.Request) {
	shortcutName := mux.Vars(r)["shortcut"]

	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	wsID := middleware.GetWorkspaceID(r)

	s, err := h.getByShortcutUC.Execute(wsID, shortcutName)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}

	response.WriteSuccess(w, http.StatusOK, s)
}

func (h *MessageShortcutHandler) handleDomainError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, messageshortcutdomain.ErrNotFound):
		response.WriteError(w, http.StatusNotFound, err.Error(), nil)
	case errors.Is(err, messageshortcutdomain.ErrShortcutExists):
		response.WriteError(w, http.StatusConflict, err.Error(), nil)
	case errors.Is(err, messageshortcutdomain.ErrNameRequired),
		errors.Is(err, messageshortcutdomain.ErrShortcutRequired),
		errors.Is(err, messageshortcutdomain.ErrTextRequired),
		errors.Is(err, messageshortcutdomain.ErrInvalidType),
		errors.Is(err, messageshortcutdomain.ErrButtonsRequired),
		errors.Is(err, messageshortcutdomain.ErrTooManyButtons),
		errors.Is(err, messageshortcutdomain.ErrMediaURLRequired),
		errors.Is(err, messageshortcutdomain.ErrShortcutNoSpaces),
		errors.Is(err, messageshortcutdomain.ErrShortcutMaxLength):
		response.WriteError(w, http.StatusBadRequest, err.Error(), nil)
	default:
		response.WriteError(w, http.StatusInternalServerError, "Internal server error", nil)
	}
}
