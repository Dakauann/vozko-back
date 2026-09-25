package scheduledmessage

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/gorilla/mux"

	"vozko/delivery/http/response"
	conversationdomain "vozko/domain/conversation"
	sm "vozko/domain/scheduled_message"
	"vozko/domain/shared"
	"vozko/domain/user"
	"vozko/domain/whatsapp/template"
	wo "vozko/domain/whatsapp_outreach"
	"vozko/infra/http/middleware"
)

const idempotencyHeader = "Idempotency-Key"

type ScheduledMessageHandler struct {
	scheduler  sm.PersonSchedulerUseCase
	listUC     sm.ListUseCase
	authorizer conversationdomain.ConversationAuthorizer
}

func NewScheduledMessageHandler(
	scheduler sm.PersonSchedulerUseCase,
	listUC sm.ListUseCase,
	authorizer conversationdomain.ConversationAuthorizer,
) *ScheduledMessageHandler {
	return &ScheduledMessageHandler{
		scheduler:  scheduler,
		listUC:     listUC,
		authorizer: authorizer,
	}
}

// @Summary		Listar mensagens agendadas de uma conversa
// @Description	Lista as mensagens agendadas de uma conversa e o estado atual da janela de atendimento. Use o parâmetro `status` (separado por vírgula) para filtrar.
// @Tags			Mensagens Agendadas
// @Produce		json
// @Param			entryType	path		string	true	"Tipo da entrada"
// @Param			entryId		path		string	true	"ID da entrada"
// @Param			status		query		string	false	"Filtro de status: pending,sending,sent,failed,canceled"
// @Success		200	{object}	ScheduledMessageListResponse
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/conversations/{entryType}/{entryId}/scheduled-messages [get]
func (h *ScheduledMessageHandler) List(w http.ResponseWriter, r *http.Request) {
	entryType, entryID, ok := h.authorizedEntry(w, r)
	if !ok {
		return
	}

	result, err := h.listUC.ForEntry(r.Context(), entryID, entryType, parseStatuses(r.URL.Query().Get("status")))
	if err != nil {
		h.writeDomainError(w, err, sm.WindowState{})
		return
	}

	response.WriteSuccess(w, http.StatusOK, ScheduledMessageListResponse{
		ScheduledMessages: toResponses(result.Messages),
		Window:            toWindowResponse(result.Window),
	})
}

// @Summary		Agendar uma mensagem
// @Description	Agenda uma mensagem para ser enviada em uma conversa. Uma mensagem de texto ou mídia precisa cair dentro da janela de atendimento aberta. Um template (campo `template`, só no WhatsApp oficial) pode ser agendado com a janela fechada, até 30 dias à frente; ele segue as mesmas regras de um envio de template (aprovação, acesso ao template, contato bloqueado, proteção contra spam) e é cobrado no momento do envio. Agendar, reagendar ou cancelar um template exige a permissão whatsapp_templates:send. O horário precisa estar a pelo menos um minuto de distância. Envie o cabeçalho `Idempotency-Key` para que um reenvio da requisição não crie uma segunda mensagem.
// @Tags			Mensagens Agendadas
// @Accept			json
// @Produce		json
// @Param			entryType		path		string					true	"Tipo da entrada"
// @Param			entryId			path		string					true	"ID da entrada"
// @Param			Idempotency-Key	header		string					false	"Chave para tornar a criação idempotente"
// @Param			request			body		ScheduleMessageRequest	true	"Mensagem e horário do agendamento"
// @Success		201	{object}	ScheduledMessageEnvelope
// @Success		200	{object}	ScheduledMessageEnvelope	"A chave de idempotência já havia sido usada; nada novo foi criado"
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		409	{object}	WindowErrorResponse			"A janela de atendimento está fechada"
// @Failure		422	{object}	WindowErrorResponse			"O horário escolhido está fora dos limites, ou o template não pode ser enviado a este contato"
// @Security		BearerAuth
// @Router			/conversations/{entryType}/{entryId}/scheduled-messages [post]
func (h *ScheduledMessageHandler) Create(w http.ResponseWriter, r *http.Request) {
	entryType, entryID, ok := entryFromPath(w, r)
	if !ok {
		return
	}

	var req ScheduleMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteInvalidBodyError(w, map[string]string{
			"text":         "string (obrigatório se não houver media_id)",
			"scheduled_at": "RFC3339 com fuso, ex: 2026-08-13T14:30:00-03:00",
			"media_id":     "string (opcional)",
			"media_type":   "image | video | audio | document (opcional)",
			"template":     "{template_id, body_params, header_params} (opcional, só WhatsApp oficial; substitui text e mídia)",
		})
		return
	}

	in := sm.ScheduleInput{
		WorkspaceID:    middleware.GetWorkspaceID(r),
		EntryID:        entryID,
		EntryType:      entryType,
		Text:           req.Text,
		ScheduledAt:    req.ScheduledAt,
		IdempotencyKey: strings.TrimSpace(r.Header.Get(idempotencyHeader)),
	}
	if req.MediaID != nil {
		in.MediaID = *req.MediaID
	}
	if req.MediaType != nil {
		in.MediaType = *req.MediaType
	}
	if req.ReplyToMessageID != nil {
		in.ReplyToMessageID = *req.ReplyToMessageID
	}
	if req.Signed != nil {
		in.Signed = *req.Signed
	}
	if req.Template != nil {
		in.Template = &sm.TemplateContent{
			ID:           req.Template.TemplateID,
			BodyParams:   req.Template.BodyParams,
			HeaderParams: req.Template.HeaderParams,
		}
	}

	result, err := h.scheduler.Schedule(r.Context(), personFrom(r), in)
	if err != nil {
		window := sm.WindowState{}
		if result != nil {
			window = result.Window
		}
		h.writeDomainError(w, err, window)
		return
	}

	status := http.StatusCreated
	if result.AlreadyExisted {
		status = http.StatusOK
	}
	response.WriteSuccess(w, status, ScheduledMessageEnvelope{
		ScheduledMessage: toResponse(result.Message),
		Window:           toWindowResponse(result.Window),
	})
}

// @Summary		Reagendar uma mensagem
// @Description	Altera o horário de uma mensagem ainda pendente. O novo horário é validado contra a janela de atendimento como ela está agora.
// @Tags			Mensagens Agendadas
// @Accept			json
// @Produce		json
// @Param			id		path		string						true	"ID da mensagem agendada"
// @Param			request	body		RescheduleMessageRequest	true	"Novo horário"
// @Success		200	{object}	ScheduledMessageEnvelope
// @Failure		401	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Failure		409	{object}	response.ErrorResponse	"A mensagem já foi enviada, cancelada ou está em envio"
// @Failure		422	{object}	WindowErrorResponse
// @Security		BearerAuth
// @Router			/scheduled-messages/{id} [patch]
func (h *ScheduledMessageHandler) Reschedule(w http.ResponseWriter, r *http.Request) {
	var req RescheduleMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteInvalidBodyError(w, map[string]string{
			"scheduled_at": "RFC3339 com fuso, ex: 2026-08-13T16:00:00-03:00",
		})
		return
	}

	result, err := h.scheduler.Reschedule(r.Context(), personFrom(r), sm.RescheduleInput{
		ID:          mux.Vars(r)["id"],
		WorkspaceID: middleware.GetWorkspaceID(r),
		ScheduledAt: req.ScheduledAt,
	})
	if err != nil {
		window := sm.WindowState{}
		if result != nil {
			window = result.Window
		}
		h.writeDomainError(w, err, window)
		return
	}

	response.WriteSuccess(w, http.StatusOK, ScheduledMessageEnvelope{
		ScheduledMessage: toResponse(result.Message),
		Window:           toWindowResponse(result.Window),
	})
}

// @Summary		Cancelar uma mensagem agendada
// @Description	Cancela uma mensagem ainda pendente. Uma mensagem já enviada não pode ser cancelada — o cliente já a recebeu.
// @Tags			Mensagens Agendadas
// @Produce		json
// @Param			id	path	string	true	"ID da mensagem agendada"
// @Success		204	"Cancelada"
// @Failure		401	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Failure		409	{object}	response.ErrorResponse	"A mensagem já foi enviada ou já não está pendente"
// @Security		BearerAuth
// @Router			/scheduled-messages/{id} [delete]
func (h *ScheduledMessageHandler) Cancel(w http.ResponseWriter, r *http.Request) {
	err := h.scheduler.Cancel(r.Context(), personFrom(r), middleware.GetWorkspaceID(r), mux.Vars(r)["id"])
	if err != nil {
		h.writeDomainError(w, err, sm.WindowState{})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// @Summary		Listar mensagens agendadas do workspace
// @Description	Lista as mensagens agendadas de todo o workspace, com paginação.
// @Tags			Mensagens Agendadas
// @Produce		json
// @Param			status		query		string	false	"Filtro de status separado por vírgula"
// @Param			page		query		int		false	"Página (padrão 1)"
// @Param			page_size	query		int		false	"Itens por página (padrão 50, máximo 200)"
// @Success		200	{object}	WorkspaceScheduledMessagesResponse
// @Failure		401	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/scheduled-messages [get]
func (h *ScheduledMessageHandler) ListWorkspace(w http.ResponseWriter, r *http.Request) {
	page, pageSize := parsePagination(r)

	messages, total, err := h.listUC.ForWorkspace(r.Context(), middleware.GetWorkspaceID(r), sm.ListQuery{
		Statuses: parseStatuses(r.URL.Query().Get("status")),
		Page:     page,
		PageSize: pageSize,
	})
	if err != nil {
		h.writeDomainError(w, err, sm.WindowState{})
		return
	}

	totalPages := 0
	if pageSize > 0 {
		totalPages = int((total + int64(pageSize) - 1) / int64(pageSize))
	}
	response.WriteSuccess(w, http.StatusOK, WorkspaceScheduledMessagesResponse{
		ScheduledMessages: toResponses(messages),
		Page:              page,
		PageSize:          pageSize,
		TotalItems:        total,
		TotalPages:        totalPages,
	})
}

func entryFromPath(w http.ResponseWriter, r *http.Request) (entryType, entryID string, ok bool) {
	vars := mux.Vars(r)
	entryType, entryID = vars["entryType"], vars["entryId"]

	if !shared.EntryType(entryType).IsKnown() {
		response.WriteError(w, http.StatusBadRequest, "Invalid entry type", nil)
		return "", "", false
	}
	if middleware.GetClaims(r) == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return "", "", false
	}
	return entryType, entryID, true
}

func personFrom(r *http.Request) shared.Person {
	claims := middleware.GetClaims(r)
	if claims == nil {
		return shared.Person{}
	}
	return shared.Person{UserID: claims.UserID, SystemAdmin: claims.Role == string(user.RoleAdmin)}
}

func (h *ScheduledMessageHandler) authorizedEntry(w http.ResponseWriter, r *http.Request) (entryType, entryID string, ok bool) {
	entryType, entryID, ok = entryFromPath(w, r)
	if !ok {
		return "", "", false
	}
	if !personFrom(r).MayActOn(h.authorizer, middleware.GetWorkspaceID(r), entryID, entryType) {
		response.WriteError(w, http.StatusForbidden, "You don't have access to this conversation", nil)
		return "", "", false
	}
	return entryType, entryID, true
}

func (h *ScheduledMessageHandler) writeDomainError(w http.ResponseWriter, err error, window sm.WindowState) {
	switch {
	case errors.Is(err, sm.ErrWindowClosed):
		writeWindowError(w, http.StatusConflict, "window_closed", err.Error(), window)
	case errors.Is(err, sm.ErrScheduledAtPastWindow):
		writeWindowError(w, http.StatusUnprocessableEntity, "past_window", err.Error(), window)
	case errors.Is(err, sm.ErrScheduledAtTooSoon):
		writeWindowError(w, http.StatusUnprocessableEntity, "too_soon", err.Error(), window)
	case errors.Is(err, sm.ErrScheduledAtTooFar):
		writeWindowError(w, http.StatusUnprocessableEntity, "too_far", err.Error(), window)
	case errors.Is(err, sm.ErrNotFound):
		response.WriteErrorWithCode(w, http.StatusNotFound, "not_found", err.Error(), nil)
	case errors.Is(err, sm.ErrEntryAccess):
		response.WriteErrorWithCode(w, http.StatusForbidden, "forbidden", "You don't have access to this conversation", nil)
	case errors.Is(err, sm.ErrTemplatePermission):
		response.WriteErrorWithCode(w, http.StatusForbidden, "template_forbidden", err.Error(), nil)
	case errors.Is(err, wo.ErrConversationNotFound):
		response.WriteErrorWithCode(w, http.StatusNotFound, "not_found", err.Error(), nil)
	case errors.Is(err, sm.ErrTemplatesUnsupported):
		response.WriteErrorWithCode(w, http.StatusUnprocessableEntity, "templates_unsupported", err.Error(), nil)
	case errors.Is(err, template.ErrTemplateParamsMismatch):
		response.WriteErrorWithCode(w, http.StatusUnprocessableEntity, "template_params", err.Error(), nil)
	case errors.Is(err, template.ErrTemplateNotSendable),
		errors.Is(err, template.ErrTemplatePhoneMismatch),
		errors.Is(err, wo.ErrTemplateForbidden),
		errors.Is(err, wo.ErrTemplateNotFound):
		response.WriteErrorWithCode(w, http.StatusUnprocessableEntity, "template_unavailable", err.Error(), nil)
	case errors.Is(err, wo.ErrLeadBlocked):
		response.WriteErrorWithCode(w, http.StatusUnprocessableEntity, "contact_blocked", err.Error(), nil)
	case errors.Is(err, wo.ErrWithinSpamWindow):
		response.WriteErrorWithCode(w, http.StatusUnprocessableEntity, "spam_window", err.Error(), nil)
	case errors.Is(err, wo.ErrPhoneNotConnected),
		errors.Is(err, wo.ErrBusinessPhoneNotFound):
		response.WriteErrorWithCode(w, http.StatusUnprocessableEntity, "number_unavailable", err.Error(), nil)
	case errors.Is(err, sm.ErrNotPending):
		response.WriteErrorWithCode(w, http.StatusConflict, "not_pending", err.Error(), nil)
	case errors.Is(err, sm.ErrContentRequired),
		errors.Is(err, sm.ErrKindInvalid),
		errors.Is(err, sm.ErrKindMismatch),
		errors.Is(err, sm.ErrTemplateRequired),
		errors.Is(err, sm.ErrTemplateWithFreeContent),
		errors.Is(err, sm.ErrEntryIDRequired),
		errors.Is(err, sm.ErrEntryTypeInvalid),
		errors.Is(err, sm.ErrWorkspaceRequired),
		errors.Is(err, sm.ErrSenderRequired):
		response.WriteErrorWithCode(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
	default:
		response.WriteError(w, http.StatusInternalServerError, "Internal server error", nil)
	}
}

func writeWindowError(w http.ResponseWriter, status int, code, message string, window sm.WindowState) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(WindowErrorResponse{
		Error:   true,
		Code:    code,
		Message: message,
		Window:  toWindowResponse(window),
	})
}
