package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gorilla/mux"

	"vozko/delivery/http/response"
	"vozko/domain/aichat"
	copilot_domain "vozko/domain/copilot"
	"vozko/infra/http/middleware"
	aichat_usecase "vozko/usecases/aichat"
	copilot_usecase "vozko/usecases/copilot"
)

const (
	chatDefaultPageSize = 30
	chatMaxPageSize     = 100
)

type AIChatHandler struct {
	svc     *aichat_usecase.Service
	copilot *copilot_usecase.Service
}

func NewAIChatHandler(svc *aichat_usecase.Service, copilot *copilot_usecase.Service) *AIChatHandler {
	return &AIChatHandler{svc: svc, copilot: copilot}
}

type threadDTO struct {
	ID            string  `json:"id"`
	Title         string  `json:"title"`
	Model         string  `json:"model"`
	LastMessageAt *string `json:"lastMessageAt,omitempty"`
	CreatedAt     string  `json:"createdAt"`
}

type toolActivityDTO struct {
	Name    string `json:"name"`
	Summary string `json:"summary"`
	Ok      bool   `json:"ok"`
}

type messageDTO struct {
	ID        string            `json:"id"`
	Role      string            `json:"role"`
	Content   string            `json:"content"`
	Model     string            `json:"model,omitempty"`
	Reasoning string            `json:"reasoning,omitempty"`
	Tools     []toolActivityDTO `json:"tools,omitempty"`
	CreatedAt string            `json:"createdAt"`
}

func (h *AIChatHandler) CreateThread(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	workspaceID := middleware.GetWorkspaceID(r)

	var body struct {
		Model string `json:"model"`
		Title string `json:"title"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)

	thread, err := h.svc.CreateThread(workspaceID, claims.UserID, body.Model, body.Title)
	if err != nil {
		h.writeError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusCreated, toThreadDTO(thread))
}

func (h *AIChatHandler) ListThreads(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	workspaceID := middleware.GetWorkspaceID(r)
	limit, offset, page, pageSize := parseChatPagination(r)

	threads, total, err := h.svc.ListThreads(workspaceID, claims.UserID, limit, offset)
	if err != nil {
		h.writeError(w, err)
		return
	}

	items := make([]threadDTO, len(threads))
	for i, t := range threads {
		items[i] = toThreadDTO(t)
	}
	response.WriteSuccess(w, http.StatusOK, map[string]any{
		"items": items, "total": total, "page": page, "pageSize": pageSize,
	})
}

func (h *AIChatHandler) ListMessages(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	workspaceID := middleware.GetWorkspaceID(r)
	threadID := mux.Vars(r)["id"]
	limit, offset, page, pageSize := parseChatPagination(r)

	msgs, total, err := h.svc.ListMessages(workspaceID, claims.UserID, threadID, limit, offset)
	if err != nil {
		h.writeError(w, err)
		return
	}

	items := make([]messageDTO, len(msgs))
	for i, m := range msgs {
		items[i] = toMessageDTO(m)
	}
	response.WriteSuccess(w, http.StatusOK, map[string]any{
		"items": items, "total": total, "page": page, "pageSize": pageSize,
	})
}

func (h *AIChatHandler) RenameThread(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	workspaceID := middleware.GetWorkspaceID(r)
	threadID := mux.Vars(r)["id"]

	var body struct {
		Title string `json:"title"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.WriteError(w, http.StatusBadRequest, "corpo inválido", nil)
		return
	}
	if err := h.svc.RenameThread(workspaceID, claims.UserID, threadID, body.Title); err != nil {
		h.writeError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *AIChatHandler) DeleteThread(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	workspaceID := middleware.GetWorkspaceID(r)
	threadID := mux.Vars(r)["id"]

	if err := h.svc.DeleteThread(workspaceID, claims.UserID, threadID); err != nil {
		h.writeError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, map[string]any{"ok": true})
}

// StreamMessage handles POST /chat/threads/{id}/messages as Server-Sent Events.
// Auth + plan/balance gating happen BEFORE the stream headers (via the aichat
// Precheck) so failures return real HTTP status codes; once streaming starts the
// copilot drives the agentic turn and each engine event is relayed as an SSE frame
// ({type, payload}). Thinking is streamed live (reasoning_delta); the final reply
// is persisted to the thread by the copilot Service.
func (h *AIChatHandler) StreamMessage(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	workspaceID := middleware.GetWorkspaceID(r)
	threadID := mux.Vars(r)["id"]

	var body struct {
		Content string `json:"content"`
		Model   string `json:"model"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.WriteError(w, http.StatusBadRequest, "corpo inválido", nil)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		response.WriteError(w, http.StatusInternalServerError, "streaming não suportado", nil)
		return
	}

	// Gate before any stream bytes so 402/403/404 surface as normal HTTP errors.
	thread, err := h.svc.Precheck(workspaceID, claims.UserID, threadID)
	if err != nil {
		h.writeError(w, err)
		return
	}
	if m := strings.TrimSpace(body.Model); m != "" {
		thread.Model = m
	}

	emit := startChatSSE(w, flusher)
	r = withDepartmentCreationScope(r, "")
	if err := h.copilot.Stream(r.Context(), thread, body.Content, copilotCtx(r, claims.UserID, workspaceID), emit); err != nil {
		emit("error", map[string]any{"error": err.Error()})
	}
}

// ApproveAction runs a previously-proposed mutation after the user confirms it,
// streaming the result. POST /chat/threads/{id}/actions/{actionId}/approve.
func (h *AIChatHandler) ApproveAction(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	workspaceID := middleware.GetWorkspaceID(r)
	threadID := mux.Vars(r)["id"]
	actionID := mux.Vars(r)["actionId"]

	flusher, ok := w.(http.Flusher)
	if !ok {
		response.WriteError(w, http.StatusInternalServerError, "streaming não suportado", nil)
		return
	}
	thread, err := h.svc.Precheck(workspaceID, claims.UserID, threadID)
	if err != nil {
		h.writeError(w, err)
		return
	}
	emit := startChatSSE(w, flusher)
	r = withDepartmentCreationScope(r, "")
	if err := h.copilot.Approve(r.Context(), thread, actionID, copilotCtx(r, claims.UserID, workspaceID), emit); err != nil {
		emit("error", map[string]any{"error": err.Error()})
	}
}

// RejectAction discards a proposed mutation. POST /chat/threads/{id}/actions/{actionId}/reject.
func (h *AIChatHandler) RejectAction(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	workspaceID := middleware.GetWorkspaceID(r)
	threadID := mux.Vars(r)["id"]
	actionID := mux.Vars(r)["actionId"]

	flusher, ok := w.(http.Flusher)
	if !ok {
		response.WriteError(w, http.StatusInternalServerError, "streaming não suportado", nil)
		return
	}
	thread, err := h.svc.Precheck(workspaceID, claims.UserID, threadID)
	if err != nil {
		h.writeError(w, err)
		return
	}
	emit := startChatSSE(w, flusher)
	if err := h.copilot.Reject(r.Context(), thread, actionID, emit); err != nil {
		emit("error", map[string]any{"error": err.Error()})
	}
}

func copilotCtx(r *http.Request, userID, workspaceID string) copilot_domain.Context {
	// Department scope mirrors the rest of the app: departmentFilterIDs returns nil
	// for owners/admins (all departments) or the member's allowed department IDs for
	// a restricted member, so the copilot can only see/touch what the user can. The
	// RBAC gate (CheckAccess) looks the workspace role up itself.
	return copilot_domain.Context{
		WorkspaceID: workspaceID,
		UserID:      userID,
		DeptScope:   departmentFilterIDs(r),
	}
}

// startChatSSE writes the SSE headers and returns an emit that relays each copilot
// engine event as a `data: {"type":<type>,"payload":<payload>}` frame.
func startChatSSE(w http.ResponseWriter, flusher http.Flusher) func(string, interface{}) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // disable proxy buffering (nginx)
	w.WriteHeader(http.StatusOK)
	flusher.Flush()
	return func(eventType string, payload interface{}) {
		data, err := json.Marshal(map[string]interface{}{"type": eventType, "payload": payload})
		if err != nil {
			return
		}
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	}
}

func (h *AIChatHandler) writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, aichat.ErrThreadNotFound):
		response.WriteError(w, http.StatusNotFound, "conversa não encontrada", nil)
	case errors.Is(err, aichat_usecase.ErrForbidden):
		response.WriteError(w, http.StatusForbidden, "sem acesso a esta conversa", nil)
	case errors.Is(err, aichat_usecase.ErrNoSubscription):
		response.WriteError(w, http.StatusPaymentRequired, "plano ativo necessário", nil)
	case errors.Is(err, aichat_usecase.ErrInsufficientBalance):
		response.WriteError(w, http.StatusPaymentRequired, "saldo insuficiente", nil)
	case errors.Is(err, aichat_usecase.ErrEmptyMessage):
		response.WriteError(w, http.StatusBadRequest, "mensagem vazia", nil)
	default:
		response.WriteError(w, http.StatusInternalServerError, "erro interno", nil)
	}
}

func parseChatPagination(r *http.Request) (limit, offset, page, pageSize int) {
	page = 1
	pageSize = chatDefaultPageSize
	if v := r.URL.Query().Get("page"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			page = n
		}
	}
	if v := r.URL.Query().Get("pageSize"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			pageSize = n
		}
	}
	if pageSize > chatMaxPageSize {
		pageSize = chatMaxPageSize
	}
	return pageSize, (page - 1) * pageSize, page, pageSize
}

func toThreadDTO(t *aichat.Thread) threadDTO {
	dto := threadDTO{
		ID:        t.ID,
		Title:     t.Title,
		Model:     t.Model,
		CreatedAt: t.CreatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
	}
	if t.LastMessageAt != nil {
		s := t.LastMessageAt.UTC().Format("2006-01-02T15:04:05Z07:00")
		dto.LastMessageAt = &s
	}
	return dto
}

func toMessageDTO(m *aichat.Message) messageDTO {
	dto := messageDTO{
		ID:        m.ID,
		Role:      string(m.Role),
		Content:   m.Content,
		Model:     m.Model,
		CreatedAt: m.CreatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
	}
	if len(m.Reasoning) > 0 {
		var reasoning string
		if json.Unmarshal(m.Reasoning, &reasoning) == nil {
			dto.Reasoning = reasoning
		}
	}
	if len(m.ToolCalls) > 0 {
		_ = json.Unmarshal(m.ToolCalls, &dto.Tools)
	}
	return dto
}
