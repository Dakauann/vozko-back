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
	"vozko/infra/http/middleware"
)

// idempotencyHeader lets a client retry a create without producing a second
// message to the customer. See the create handler.
const idempotencyHeader = "Idempotency-Key"

type ScheduledMessageHandler struct {
	scheduleUC   sm.ScheduleUseCase
	rescheduleUC sm.RescheduleUseCase
	cancelUC     sm.CancelUseCase
	listUC       sm.ListUseCase
	authorizer   conversationdomain.ConversationAuthorizer
}

// NewScheduledMessageHandler wires the HTTP surface.
//
// The authorizer is required and used on every entry-scoped route: the
// permission middleware answers "may this role schedule messages", which is not
// the same question as "may this user see THIS conversation".
func NewScheduledMessageHandler(
	scheduleUC sm.ScheduleUseCase,
	rescheduleUC sm.RescheduleUseCase,
	cancelUC sm.CancelUseCase,
	listUC sm.ListUseCase,
	authorizer conversationdomain.ConversationAuthorizer,
) *ScheduledMessageHandler {
	return &ScheduledMessageHandler{
		scheduleUC:   scheduleUC,
		rescheduleUC: rescheduleUC,
		cancelUC:     cancelUC,
		listUC:       listUC,
		authorizer:   authorizer,
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
// @Description	Agenda uma mensagem para ser enviada em uma conversa. O horário precisa estar dentro da janela de atendimento aberta e a pelo menos um minuto de distância. Envie o cabeçalho `Idempotency-Key` para que um reenvio da requisição não crie uma segunda mensagem.
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
// @Failure		422	{object}	WindowErrorResponse			"O horário escolhido está fora dos limites"
// @Security		BearerAuth
// @Router			/conversations/{entryType}/{entryId}/scheduled-messages [post]
func (h *ScheduledMessageHandler) Create(w http.ResponseWriter, r *http.Request) {
	entryType, entryID, ok := h.authorizedEntry(w, r)
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
		})
		return
	}

	claims := middleware.GetClaims(r)
	in := sm.ScheduleInput{
		WorkspaceID:     middleware.GetWorkspaceID(r),
		EntryID:         entryID,
		EntryType:       entryType,
		CreatedByUserID: claims.UserID,
		Text:            req.Text,
		ScheduledAt:     req.ScheduledAt,
		IdempotencyKey:  strings.TrimSpace(r.Header.Get(idempotencyHeader)),
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

	result, err := h.scheduleUC.Execute(r.Context(), in)
	if err != nil {
		window := sm.WindowState{}
		if result != nil {
			window = result.Window
		}
		h.writeDomainError(w, err, window)
		return
	}

	// A replayed idempotency key created nothing, so it answers 200. Reporting
	// 201 would tell a retrying client it had just made a second message.
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

	result, err := h.rescheduleUC.Execute(r.Context(), sm.RescheduleInput{
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
	err := h.cancelUC.Execute(r.Context(), middleware.GetWorkspaceID(r), mux.Vars(r)["id"])
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

// authorizedEntry validates the entry pair and the caller's access to it.
//
// The route's permission gate answers "may this ROLE schedule messages"; this
// answers "may this USER see this conversation". Both are needed: department
// scoping means a member with the permission can still be outside a given
// conversation.
func (h *ScheduledMessageHandler) authorizedEntry(w http.ResponseWriter, r *http.Request) (entryType, entryID string, ok bool) {
	vars := mux.Vars(r)
	entryType, entryID = vars["entryType"], vars["entryId"]

	if !shared.EntryType(entryType).IsKnown() {
		response.WriteError(w, http.StatusBadRequest, "Invalid entry type", nil)
		return "", "", false
	}

	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return "", "", false
	}

	workspaceID := middleware.GetWorkspaceID(r)
	isAdmin := claims.Role == string(user.RoleAdmin)
	if !h.authorizer.CanAccessEntry(claims.UserID, workspaceID, entryID, entryType, isAdmin) {
		response.WriteError(w, http.StatusForbidden, "You don't have access to this conversation", nil)
		return "", "", false
	}

	return entryType, entryID, true
}

// writeDomainError maps a domain error onto a status and a machine-readable
// code.
//
// The window travels with the two refusals that are ABOUT the window, because a
// refusal that does not name the boundary makes the operator's next attempt a
// guess.
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
	case errors.Is(err, sm.ErrNotPending):
		response.WriteErrorWithCode(w, http.StatusConflict, "not_pending", err.Error(), nil)
	case errors.Is(err, sm.ErrContentRequired),
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
