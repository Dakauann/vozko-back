package conversation

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"

	"vozko/delivery/http/response"
	conversationdomain "vozko/domain/conversation"
	ce "vozko/domain/conversation_event"
	"vozko/domain/shared"
	"vozko/domain/user"
	"vozko/infra/http/middleware"
	ia_usecase "vozko/usecases/inbox_assignment"
)

type ConversationHandler struct {
	uploadMediaUC         conversationdomain.UploadConversationMediaUseCase
	getMediaUC            conversationdomain.GetConversationMediaUseCase
	listEventsUC          ce.ListEventsUseCase
	requestCallPermission conversationdomain.RequestCallPermissionUseCase
	automationService     ConversationAutomationService
}

// ConversationAutomationService switches a conversation's automation for a
// caller, moving its ownership along (see ia_usecase.OperatorAutomationToggle).
type ConversationAutomationService interface {
	SetAutomation(ctx context.Context, in ia_usecase.OperatorAutomationInput) (ia_usecase.OperatorAutomationResult, error)
}

func (h *ConversationHandler) SetAutomationService(s ConversationAutomationService) {
	h.automationService = s
}

func (h *ConversationHandler) SetRequestCallPermission(uc conversationdomain.RequestCallPermissionUseCase) {
	h.requestCallPermission = uc
}

func NewConversationHandler(
	uploadMediaUC conversationdomain.UploadConversationMediaUseCase,
	getMediaUC conversationdomain.GetConversationMediaUseCase,
	listEventsUC ce.ListEventsUseCase,
) *ConversationHandler {
	return &ConversationHandler{
		uploadMediaUC: uploadMediaUC,
		getMediaUC:    getMediaUC,
		listEventsUC:  listEventsUC,
	}
}

// @Summary		Solicitar permissão para ligar
// @Description	Solicita ao cliente permissão para receber ligações pelo WhatsApp na conversa informada. A janela de 24 horas precisa estar aberta ou um modelo (template) é exigido.
// @Tags			Conversas
// @Accept			json
// @Produce		json
// @Param			entryType	path		string						true	"Tipo da entrada ('whatsapp' ou 'support')"
// @Param			entryId		path		string						true	"ID da entrada"
// @Param			request		body		CallPermissionRequestBody	false	"Texto opcional da solicitação"
// @Success		200	{object}	MessageEnvelopeResponse
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		409	{object}	response.ErrorResponse
// @Failure		501	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/conversations/{entryType}/{entryId}/call-permission-request [post]
func (h *ConversationHandler) RequestCallPermission(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}
	if h.requestCallPermission == nil {
		response.WriteError(w, http.StatusNotImplemented, "Call permission requests are not enabled", nil)
		return
	}

	vars := mux.Vars(r)
	entryType := vars["entryType"]
	entryID := vars["entryId"]
	if !shared.EntryType(entryType).IsKnown() {
		response.WriteError(w, http.StatusBadRequest, "Invalid entry type", nil)
		return
	}

	var req CallPermissionRequestBody
	_ = json.NewDecoder(r.Body).Decode(&req)

	message, err := h.requestCallPermission.RequestCallPermission(conversationdomain.RequestCallPermissionInput{
		EntryID:   entryID,
		EntryType: entryType,
		SenderID:  claims.UserID,
		BodyText:  req.BodyText,
	})
	if err != nil {
		switch err {
		case conversationdomain.ErrWindowClosed:
			response.WriteError(w, http.StatusConflict, "The 24-hour window is closed; a template is required to request permission.", nil)
		case conversationdomain.ErrWhatsAppCallNotConfigured:
			response.WriteError(w, http.StatusBadRequest, "WhatsApp calling is not configured for this conversation.", nil)
		default:
			response.WriteError(w, http.StatusInternalServerError, err.Error(), nil)
		}
		return
	}

	response.WriteSuccess(w, http.StatusOK, map[string]interface{}{
		"message": message,
	})
}

// @Summary		Consultar permissão para ligar
// @Description	Informa se o cliente da conversa permite atualmente ligações pelo WhatsApp, para que o cliente possa habilitar ou não a ação de ligar.
// @Tags			Conversas
// @Produce		json
// @Param			entryType	path		string	true	"Tipo da entrada ('whatsapp' ou 'support')"
// @Param			entryId		path		string	true	"ID da entrada"
// @Success		200	{object}	conversation.CallPermissionStatus
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Failure		501	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/conversations/{entryType}/{entryId}/call-permission [get]
func (h *ConversationHandler) GetCallPermission(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}
	if h.requestCallPermission == nil {
		response.WriteError(w, http.StatusNotImplemented, "Call permission requests are not enabled", nil)
		return
	}

	vars := mux.Vars(r)
	entryType := vars["entryType"]
	entryID := vars["entryId"]
	if !shared.EntryType(entryType).IsKnown() {
		response.WriteError(w, http.StatusBadRequest, "Invalid entry type", nil)
		return
	}

	if entryType == "support" {
		response.WriteSuccess(w, http.StatusOK, conversationdomain.CallPermissionStatus{Status: "none"})
		return
	}

	status, err := h.requestCallPermission.CallPermissionStatus(entryID, entryType)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, err.Error(), nil)
		return
	}

	response.WriteSuccess(w, http.StatusOK, status)
}

// @Summary		Enviar mídia para uma conversa
// @Description	Faz upload de um arquivo de mídia (multipart/form-data) para uma conversa e retorna o identificador da mídia, usado depois no envio da mensagem.
// @Tags			Conversas
// @Accept			mpfd
// @Produce		json
// @Param			entryType	path		string	true	"Tipo da entrada ('whatsapp' ou 'support')"
// @Param			entryId		path		string	true	"ID da entrada"
// @Param			media		formData	file	true	"Arquivo de mídia"
// @Param			mediaType	formData	string	true	"Tipo da mídia (ex.: image, video, audio, document)"
// @Success		200	{object}	conversation.ConversationMedia
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/conversations/{entryType}/{entryId}/media [post]
func (h *ConversationHandler) UploadMedia(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	vars := mux.Vars(r)
	entryType := vars["entryType"]
	entryID := vars["entryId"]

	if !shared.EntryType(entryType).IsKnown() {
		response.WriteError(w, http.StatusBadRequest, "Invalid entry type", nil)
		return
	}

	if err := r.ParseMultipartForm(32 << 20); err != nil {
		response.WriteError(w, http.StatusBadRequest, "Failed to parse multipart form", nil)
		return
	}

	file, header, err := r.FormFile("media")
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, "Unable to get media file", nil)
		return
	}
	defer file.Close()

	mediaType := r.FormValue("mediaType")
	if mediaType == "" {
		response.WriteError(w, http.StatusBadRequest, "Unable to get media type", nil)
		return
	}

	if !conversationdomain.MediaType(mediaType).Valid() {
		response.WriteError(w, http.StatusBadRequest, "Invalid media type", nil)
		return
	}

	data, err := io.ReadAll(file)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to read file", nil)
		return
	}

	media, err := h.uploadMediaUC.Execute(conversationdomain.UploadMediaInput{
		EntryID:   entryID,
		EntryType: entryType,
		MediaType: conversationdomain.MediaType(mediaType),
		Filename:  header.Filename,
		Data:      data,
		MimeType:  header.Header.Get("Content-Type"),
	})

	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, err.Error(), nil)
		return
	}

	response.WriteSuccess(w, http.StatusOK, map[string]interface{}{
		"media_id":   media.ID,
		"media_type": media.Type,
		"url":        media.URL,
		"filename":   media.OriginalFilename,
	})
}

// @Summary		Obter mídia de uma conversa
// @Description	Retorna os metadados de um arquivo de mídia de uma conversa, incluindo a URL de acesso, o tipo e o tamanho.
// @Tags			Conversas
// @Produce		json
// @Param			entryType	path		string	true	"Tipo da entrada ('whatsapp' ou 'support')"
// @Param			entryId		path		string	true	"ID da entrada"
// @Param			mediaId		path		string	true	"ID da mídia"
// @Success		200	{object}	conversation.ConversationMedia
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/conversations/{entryType}/{entryId}/media/{mediaId} [get]
func (h *ConversationHandler) GetMedia(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	vars := mux.Vars(r)
	entryType := vars["entryType"]
	entryID := vars["entryId"]
	mediaID := vars["mediaId"]

	if !shared.EntryType(entryType).IsKnown() {
		response.WriteError(w, http.StatusBadRequest, "Invalid entry type", nil)
		return
	}

	if mediaID == "" {
		response.WriteError(w, http.StatusBadRequest, "Media ID is required", nil)
		return
	}

	media, err := h.getMediaUC.Execute(mediaID)
	if err != nil {
		response.WriteError(w, http.StatusNotFound, "Media not found", nil)
		return
	}

	if media.EntryID != entryID || string(media.EntryType) != entryType {
		response.WriteError(w, http.StatusForbidden, "Media does not belong to this conversation", nil)
		return
	}

	response.WriteSuccess(w, http.StatusOK, map[string]interface{}{
		"id":         media.ID,
		"media_type": media.Type,
		"mime_type":  media.MimeType,
		"url":        media.URL,
		"filename":   media.OriginalFilename,
		"size":       media.SizeBytes,
		"created_at": media.CreatedAt,
	})
}

// @Summary		Listar eventos de uma conversa
// @Description	Retorna o histórico de eventos de uma conversa (mudanças de etapa, atribuições, entre outros), com paginação.
// @Tags			Conversas
// @Produce		json
// @Param			entryType	path		string	true	"Tipo da entrada ('whatsapp' ou 'support')"
// @Param			entryId		path		string	true	"ID da entrada"
// @Param			page		query		int		false	"Número da página"
// @Param			page_size	query		int		false	"Tamanho da página"
// @Success		200	{array}		conversation_event.ConversationEvent
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/conversations/{entryType}/{entryId}/events [get]
func (h *ConversationHandler) ListConversationEvents(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	vars := mux.Vars(r)
	entryID := vars["entryId"]
	entryType := vars["entryType"]

	if entryID == "" || entryType == "" {
		response.WriteError(w, http.StatusBadRequest, "entry_id and entry_type are required", nil)
		return
	}

	wsID := middleware.GetWorkspaceID(r)

	q := r.URL.Query()
	page := 1
	if v := q.Get("page"); v != "" {
		if p, err := strconv.Atoi(v); err == nil && p > 0 {
			page = p
		}
	}
	pageSize := 50
	if v := q.Get("page_size"); v != "" {
		if ps, err := strconv.Atoi(v); err == nil && ps > 0 && ps <= 200 {
			pageSize = ps
		}
	}

	offset := (page - 1) * pageSize
	events, totalItems, err := h.listEventsUC.Execute(wsID, entryID, entryType, pageSize, offset)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to list events: "+err.Error(), nil)
		return
	}

	totalPages := 0
	if totalItems > 0 {
		totalPages = int((totalItems + int64(pageSize) - 1) / int64(pageSize))
	}

	response.WriteSuccess(w, http.StatusOK, map[string]interface{}{
		"events":      events,
		"page":        page,
		"page_size":   pageSize,
		"total_items": totalItems,
		"total_pages": totalPages,
	})
}

type SetAutomationRequest struct {
	AutomationEnabled *bool `json:"automationEnabled"`
}

// @Summary		Ativar/desativar automação de uma conversa
// @Description	Liga ou desliga o atendimento automático desta conversa. Envie null para voltar a herdar a configuração da conta/campanha. Desligar devolve para a fila da equipe uma conversa que a IA ou o fluxo detinha; ligar devolve a conversa para a IA ou o fluxo que atende o canal. A resposta traz o responsável resultante em assigned_user_id.
// @Tags			Conversas
// @Accept			json
// @Produce		json
// @Param			entryType	path		string					true	"Tipo da entrada"
// @Param			entryId		path		string					true	"ID da entrada"
// @Param			request		body		SetAutomationRequest	true	"Novo estado"
// @Success		200			{object}	map[string]interface{}
// @Router			/conversations/{entryType}/{entryId}/automation [patch]
func (h *ConversationHandler) SetAutomation(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	vars := mux.Vars(r)
	entryType := vars["entryType"]
	entryID := vars["entryId"]

	if !shared.EntryType(entryType).IsKnown() {
		response.WriteError(w, http.StatusBadRequest, "Invalid entry type", nil)
		return
	}
	if entryID == "" {
		response.WriteError(w, http.StatusBadRequest, "entryId is required", nil)
		return
	}

	var req SetAutomationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, "Invalid request body", nil)
		return
	}

	if h.automationService == nil {
		response.WriteError(w, http.StatusInternalServerError, "Automation service not configured", nil)
		return
	}

	result, err := h.automationService.SetAutomation(r.Context(), ia_usecase.OperatorAutomationInput{
		ActorUserID: claims.UserID,
		WorkspaceID: middleware.GetWorkspaceID(r),
		IsAdmin:     claims.Role == string(user.RoleAdmin),
		EntryID:     entryID,
		EntryType:   shared.EntryType(entryType),
		Enabled:     req.AutomationEnabled,
	})
	if errors.Is(err, ia_usecase.ErrAutomationForbidden) {
		response.WriteError(w, http.StatusForbidden, "You don't have access to this conversation", nil)
		return
	}
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, err.Error(), nil)
		return
	}

	response.WriteSuccess(w, http.StatusOK, map[string]interface{}{
		"entry_id":           entryID,
		"entry_type":         entryType,
		"automation_enabled": req.AutomationEnabled,
		"assigned_user_id":   result.Owner,
	})
}
