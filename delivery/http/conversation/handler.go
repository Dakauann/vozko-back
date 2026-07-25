package conversation

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"

	"vozko/delivery/http/response"
	conversationdomain "vozko/domain/conversation"
	ce "vozko/domain/conversation_event"
	"vozko/domain/shared"
	"vozko/domain/user"
	"vozko/infra/http/middleware"
)

type ConversationHandler struct {
	sendMessageUC         conversationdomain.SendConversationMessageUseCase
	uploadMediaUC         conversationdomain.UploadConversationMediaUseCase
	getMediaUC            conversationdomain.GetConversationMediaUseCase
	inboxService          conversationdomain.InboxService
	searchMessagesUC      conversationdomain.SearchMessagesByEntryUseCase
	listEventsUC          ce.ListEventsUseCase
	requestCallPermission conversationdomain.RequestCallPermissionUseCase
}

func (h *ConversationHandler) SetRequestCallPermission(uc conversationdomain.RequestCallPermissionUseCase) {
	h.requestCallPermission = uc
}

func NewConversationHandler(
	sendMessageUC conversationdomain.SendConversationMessageUseCase,
	uploadMediaUC conversationdomain.UploadConversationMediaUseCase,
	getMediaUC conversationdomain.GetConversationMediaUseCase,
	inboxService conversationdomain.InboxService,
	searchMessagesUC conversationdomain.SearchMessagesByEntryUseCase,
	listEventsUC ce.ListEventsUseCase,
) *ConversationHandler {
	return &ConversationHandler{
		sendMessageUC:    sendMessageUC,
		uploadMediaUC:    uploadMediaUC,
		getMediaUC:       getMediaUC,
		inboxService:     inboxService,
		searchMessagesUC: searchMessagesUC,
		listEventsUC:     listEventsUC,
	}
}

// @Summary		Enviar mensagem em uma conversa
// @Description	Envia uma mensagem de texto e/ou mídia em uma conversa (entryType 'whatsapp' ou 'support'). É obrigatório informar o texto ou o media_id.
// @Tags			Conversas
// @Accept			json
// @Produce		json
// @Param			entryType	path		string				true	"Tipo da entrada ('whatsapp' ou 'support')"
// @Param			entryId		path		string				true	"ID da entrada"
// @Param			request		body		SendMessageRequest	true	"Conteúdo da mensagem a enviar"
// @Success		200	{object}	MessageEnvelopeResponse
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/conversations/{entryType}/{entryId}/messages [post]
func (h *ConversationHandler) SendMessage(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	vars := mux.Vars(r)
	entryType := vars["entryType"]
	entryID := vars["entryId"]

	if entryType != "voice" && entryType != "whatsapp" && entryType != "support" {
		response.WriteError(w, http.StatusBadRequest, "Invalid entry type", nil)
		return
	}

	var req SendMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, "Invalid request body", nil)
		return
	}

	if req.Text == "" && req.MediaID == nil {
		response.WriteError(w, http.StatusBadRequest, "text or media_id is required", nil)
		return
	}

	input := conversationdomain.SendMessageInput{
		EntryID:          entryID,
		EntryType:        entryType,
		Text:             req.Text,
		SenderID:         claims.UserID,
		ReplyToMessageID: req.ReplyToMessageID,
	}

	if req.MediaID != nil {
		input.MediaID = req.MediaID
	}
	if req.MediaType != nil {
		mt := conversationdomain.MediaType(*req.MediaType)
		input.MediaType = &mt
	}

	message, err := h.sendMessageUC.Execute(input)
	if err != nil {
		if err == conversationdomain.ErrUnauthorized {
			response.WriteError(w, http.StatusForbidden, "You don't have access to this conversation", nil)
			return
		}
		if err == conversationdomain.ErrConversationNotFound {
			response.WriteError(w, http.StatusNotFound, "Conversation not found", nil)
			return
		}
		response.WriteError(w, http.StatusInternalServerError, err.Error(), nil)
		return
	}

	response.WriteSuccess(w, http.StatusOK, map[string]interface{}{
		"message": message,
	})
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
	if entryType != "voice" && entryType != "whatsapp" && entryType != "support" {
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
	if entryType != "voice" && entryType != "whatsapp" && entryType != "support" {
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

	if entryType != "voice" && entryType != "whatsapp" && entryType != "support" {
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

	if entryType != "voice" && entryType != "whatsapp" && entryType != "support" {
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

// @Summary		Buscar conversas na caixa de entrada
// @Description	Lista e filtra as conversas da caixa de entrada de uma campanha (voice, whatsapp ou support), com paginação e diversos filtros (etapa, canal, período, mensagens não lidas, entre outros).
// @Tags			Conversas
// @Produce		json
// @Param			campaign_id			query	string	true	"ID da campanha"
// @Param			campaign_type		query	string	true	"Tipo da campanha ('whatsapp' ou 'support')"
// @Param			query				query	string	false	"Termo de busca"
// @Param			stage_id			query	string	false	"Filtrar por etapa"
// @Param			tag_name			query	string	false	"Filtrar por nome de etapa"
// @Param			message_search		query	string	false	"Buscar no conteúdo das mensagens"
// @Param			channel				query	string	false	"Filtrar por canal"
// @Param			date_from			query	string	false	"Data inicial (RFC3339)"
// @Param			date_to				query	string	false	"Data final (RFC3339)"
// @Param			min_message_count	query	int		false	"Mínimo de mensagens"
// @Param			max_message_count	query	int		false	"Máximo de mensagens"
// @Param			window_open			query	bool	false	"Somente conversas com janela aberta"
// @Param			has_unread			query	bool	false	"Somente conversas com mensagens não lidas"
// @Param			page				query	int		false	"Número da página"
// @Param			page_size			query	int		false	"Tamanho da página"
// @Success		200	{array}		conversation.InboxEntry
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/conversations/inbox/search [get]
func (h *ConversationHandler) SearchInbox(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	q := r.URL.Query()
	campaignID := q.Get("campaign_id")
	campaignType := q.Get("campaign_type")

	if campaignID == "" || campaignType == "" {
		response.WriteError(w, http.StatusBadRequest, "campaign_id and campaign_type are required", nil)
		return
	}
	if campaignType != "voice" && campaignType != "whatsapp" && campaignType != "support" {
		response.WriteError(w, http.StatusBadRequest, "campaign_type must be 'voice', 'whatsapp', or 'support'", nil)
		return
	}

	page := 1
	if v := q.Get("page"); v != "" {
		if p, err := strconv.Atoi(v); err == nil && p > 0 {
			page = p
		}
	}
	pageSize := conversationdomain.DefaultInboxPageSize
	if v := q.Get("page_size"); v != "" {
		if ps, err := strconv.Atoi(v); err == nil && ps > 0 {
			pageSize = ps
		}
	}
	if pageSize > conversationdomain.MaxInboxPageSize {
		pageSize = conversationdomain.MaxInboxPageSize
	}

	var selectedDepartmentID string
	if filter := middleware.GetDepartmentFilter(r); filter != nil && filter.SelectedDepartmentID != nil {
		selectedDepartmentID = *filter.SelectedDepartmentID
	}

	input := conversationdomain.SearchInboxInput{
		UserID:               claims.UserID,
		CampaignID:           campaignID,
		CampaignType:         campaignType,
		SelectedDepartmentID: selectedDepartmentID,
		Query:                q.Get("query"),
		StageID:              q.Get("stage_id"),
		StageName:            q.Get("tag_name"),
		MessageSearch:        q.Get("message_search"),
		Channel:              q.Get("channel"),
		IsAdmin:              claims.Role == "admin",
		Page:                 page,
		PageSize:             pageSize,
	}

	if v := q.Get("date_from"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			input.DateFrom = &t
		}
	}
	if v := q.Get("date_to"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			input.DateTo = &t
		}
	}

	if v := q.Get("min_message_count"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			input.MinMessageCount = &n
		}
	}
	if v := q.Get("max_message_count"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			input.MaxMessageCount = &n
		}
	}
	if v := q.Get("window_open"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			input.WindowOpen = &b
		}
	}
	if v := q.Get("has_unread"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			input.HasUnread = &b
		}
	}

	if h.inboxService == nil {
		response.WriteError(w, http.StatusInternalServerError, "Inbox service not configured", nil)
		return
	}

	entries, totalItems, err := h.inboxService.SearchInbox(claims.UserID, input)
	if err != nil {
		if strings.Contains(err.Error(), "unauthorized") {
			response.WriteError(w, http.StatusForbidden, "Forbidden", nil)
			return
		}
		response.WriteError(w, http.StatusInternalServerError, "Search failed: "+err.Error(), nil)
		return
	}

	totalPages := 0
	if totalItems > 0 {
		totalPages = int((totalItems + int64(pageSize) - 1) / int64(pageSize))
	}

	response.WriteSuccess(w, http.StatusOK, map[string]interface{}{
		"entries":     entries,
		"page":        page,
		"page_size":   pageSize,
		"total_items": totalItems,
		"total_pages": totalPages,
		"query":       input.Query,
		"filters": map[string]interface{}{
			"stage_id":          input.StageID,
			"tag_name":          input.StageName,
			"min_message_count": input.MinMessageCount,
			"max_message_count": input.MaxMessageCount,
			"message_search":    input.MessageSearch,
			"window_open":       input.WindowOpen,
			"has_unread":        input.HasUnread,
			"channel":           input.Channel,
			"date_from":         input.DateFrom,
			"date_to":           input.DateTo,
		},
	})
}

// @Summary		Reabrir janela de conversa
// @Description	Envia um modelo (template) do WhatsApp para reabrir a janela de atendimento de 24 horas de uma conversa. A janela reabre quando o cliente responde.
// @Tags			Conversas
// @Accept			json
// @Produce		json
// @Param			entryType	path		string				true	"Tipo da entrada ('whatsapp' ou 'support')"
// @Param			entryId		path		string				true	"ID da entrada"
// @Param			request		body		ReopenWindowRequest	true	"Modelo e parâmetros a enviar"
// @Success		200	{object}	object
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/conversations/{entryType}/{entryId}/reopen-window [post]
func (h *ConversationHandler) ReopenWindow(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	vars := mux.Vars(r)
	entryType := vars["entryType"]
	entryID := vars["entryId"]

	if entryType != "voice" && entryType != "whatsapp" && entryType != "support" {
		response.WriteError(w, http.StatusBadRequest, "Invalid entry type", nil)
		return
	}

	var req ReopenWindowRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, "Invalid request body", nil)
		return
	}

	if req.TemplateID == "" {
		response.WriteError(w, http.StatusBadRequest, "template_id is required", nil)
		return
	}

	if h.inboxService == nil {
		response.WriteError(w, http.StatusInternalServerError, "Inbox service not configured", nil)
		return
	}

	isAdmin := claims.Role == string(user.RoleAdmin)
	wsID := middleware.GetWorkspaceID(r)
	messageID, err := h.inboxService.SendTemplateForEntry(entryID, entryType, req.TemplateID, req.Parameters, claims.UserID, wsID, isAdmin)
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, err.Error(), nil)
		return
	}

	response.WriteSuccess(w, http.StatusOK, map[string]interface{}{
		"message_id":  messageID,
		"entry_id":    entryID,
		"entry_type":  entryType,
		"template_id": req.TemplateID,
		"note":        "Template sent. Window will reopen when the user responds.",
	})
}

// @Summary		Buscar mensagens de uma conversa
// @Description	Busca mensagens de uma conversa específica por termo, com paginação. O parâmetro query é obrigatório.
// @Tags			Conversas
// @Produce		json
// @Param			entryType	path		string	true	"Tipo da entrada ('whatsapp' ou 'support')"
// @Param			entryId		path		string	true	"ID da entrada"
// @Param			query		query		string	true	"Termo de busca"
// @Param			page		query		int		false	"Número da página"
// @Param			page_size	query		int		false	"Tamanho da página"
// @Success		200	{object}	SearchMessagesResponse
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/conversations/{entryType}/{entryId}/messages/search [get]
func (h *ConversationHandler) SearchMessages(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	vars := mux.Vars(r)
	entryType := vars["entryType"]
	entryID := vars["entryId"]

	if entryType != "voice" && entryType != "whatsapp" && entryType != "support" {
		response.WriteError(w, http.StatusBadRequest, "Invalid entry type", nil)
		return
	}

	q := r.URL.Query()
	query := q.Get("query")
	if query == "" {
		response.WriteError(w, http.StatusBadRequest, "query parameter is required", nil)
		return
	}

	page := 1
	if v := q.Get("page"); v != "" {
		if p, err := strconv.Atoi(v); err == nil && p > 0 {
			page = p
		}
	}
	pageSize := 50
	if v := q.Get("page_size"); v != "" {
		if ps, err := strconv.Atoi(v); err == nil && ps > 0 {
			pageSize = ps
		}
	}
	if pageSize > 100 {
		pageSize = 100
	}

	if h.searchMessagesUC == nil {
		response.WriteError(w, http.StatusInternalServerError, "Message search not configured", nil)
		return
	}

	input := conversationdomain.SearchMessagesByEntryInput{
		EntryID:   entryID,
		EntryType: shared.EntryType(entryType),
		Query:     query,
		Page:      page,
		PageSize:  pageSize,
	}

	messages, totalItems, err := h.searchMessagesUC.Execute(input)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Search failed: "+err.Error(), nil)
		return
	}

	totalPages := 0
	if totalItems > 0 {
		totalPages = int((totalItems + int64(pageSize) - 1) / int64(pageSize))
	}

	response.WriteSuccess(w, http.StatusOK, map[string]interface{}{
		"messages":    messages,
		"page":        page,
		"page_size":   pageSize,
		"total_items": totalItems,
		"total_pages": totalPages,
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
