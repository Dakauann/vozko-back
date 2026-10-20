package conversationnotehttp

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"

	"vozko/delivery/conversationnoteview"
	"vozko/delivery/http/response"
	"vozko/domain/conversation"
	"vozko/domain/conversationnote"
	"vozko/domain/shared"
	"vozko/domain/user"
	"vozko/infra/http/middleware"
)

type Handler struct {
	notes conversationnote.UseCases
}

func NewHandler(notes conversationnote.UseCases) *Handler {
	return &Handler{notes: notes}
}

// @Summary		Listar notas internas de uma conversa
// @Description	Lista as notas internas da conversa, da mais nova para a mais antiga. Notas internas são só da equipe: nunca vão para o contato, nem para a IA, a análise, os fluxos, as exportações ou o chat do site. Vale o mesmo acesso de abrir a conversa (departamento, responsável e `view_others`) mais `conversations:read`. Para a próxima página, envie em `before` o `created_at` da nota mais antiga recebida; `has_more` diz se ainda há notas mais antigas. Notas apagadas não aparecem.
// @Tags			Notas Internas
// @Produce		json
// @Param			entryType	path		string	true	"Canal da conversa: whatsapp, unofficial_whatsapp, instagram, facebook, telegram ou webchat"
// @Param			entryId		path		string	true	"ID da conversa"
// @Param			before		query		string	false	"Só notas criadas antes deste instante (RFC 3339, aceita nanossegundos)"
// @Param			limit		query		int		false	"Quantidade (padrão 50, máximo 100)"
// @Success		200	{object}	NoteListResponse
// @Failure		400	{object}	response.ErrorResponse	"invalid_entry_type, invalid_before ou invalid_limit"
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse	"forbidden: sem acesso a esta conversa"
// @Security		BearerAuth
// @Router			/conversations/{entryType}/{entryId}/notes [get]
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	entry, ok := entryFromPath(w, r)
	if !ok {
		return
	}
	query := conversationnote.PageQuery{WorkspaceID: middleware.GetWorkspaceID(r), Entry: entry}
	if query.Before, ok = parseBefore(w, r); !ok {
		return
	}
	if query.Limit, ok = parseLimit(w, r); !ok {
		return
	}

	page, err := h.notes.List(r.Context(), personFrom(r), query)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, NoteListResponse{Notes: conversationnoteview.ListOf(page.Notes), HasMore: page.HasMore})
}

// @Summary		Criar uma nota interna
// @Description	Grava uma nota interna na conversa, visível só para a equipe que pode ver a conversa. O texto tem de 1 a 4000 caracteres (sem contar espaços nas pontas). `mentions` traz até 20 IDs de usuários (repetidos contam uma vez); IDs que não são membros do workspace são descartados sem erro e não ficam gravados. Cada mencionado que pode ver a conversa recebe uma notificação na categoria `conversations`, com o autor e o contato no título e um link para a conversa (nunca o texto da nota); quem não pode ver a conversa não é notificado nem fica sabendo da nota. Os inscritos na conversa recebem `conversation:note` pelo WebSocket de conversas.
// @Tags			Notas Internas
// @Accept			json
// @Produce		json
// @Param			entryType	path		string				true	"Canal da conversa"
// @Param			entryId		path		string				true	"ID da conversa"
// @Param			request		body		CreateNoteRequest	true	"Texto e menções"
// @Success		201	{object}	NoteEnvelope
// @Failure		400	{object}	response.ErrorResponse	"invalid_entry_type, body_required, body_too_long ou too_many_mentions"
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse	"forbidden: sem acesso a esta conversa"
// @Security		BearerAuth
// @Router			/conversations/{entryType}/{entryId}/notes [post]
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	entry, ok := entryFromPath(w, r)
	if !ok {
		return
	}
	var req CreateNoteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteInvalidBodyError(w, map[string]string{
			"body":     "string (obrigatório, de 1 a 4000 caracteres)",
			"mentions": "string[] (opcional, até 20 IDs de usuários)",
		})
		return
	}

	note, err := h.notes.Create(r.Context(), personFrom(r), conversationnote.Draft{
		WorkspaceID: middleware.GetWorkspaceID(r),
		Entry:       entry,
		Body:        req.Body,
		Mentions:    req.Mentions,
	})
	if err != nil {
		writeDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusCreated, NoteEnvelope{Note: conversationnoteview.Of(note)})
}

// @Summary		Apagar uma nota interna
// @Description	Apaga uma nota interna. Só o autor apaga, e só enquanto ainda pode ver a conversa. Os inscritos na conversa recebem `conversation:note_deleted`. Uma nota que não existe, já foi apagada, é de outro workspace ou de uma conversa que você não vê responde 404.
// @Tags			Notas Internas
// @Produce		json
// @Param			id	path	string	true	"ID da nota"
// @Success		204	"Apagada"
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse	"not_author: só o autor apaga a nota"
// @Failure		404	{object}	response.ErrorResponse	"not_found"
// @Security		BearerAuth
// @Router			/conversation-notes/{id} [delete]
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	if middleware.GetClaims(r) == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}
	if err := h.notes.Delete(r.Context(), personFrom(r), middleware.GetWorkspaceID(r), mux.Vars(r)["id"]); err != nil {
		writeDomainError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func entryFromPath(w http.ResponseWriter, r *http.Request) (shared.EntryRef, bool) {
	vars := mux.Vars(r)
	entry := shared.EntryRef{
		EntryID:   strings.TrimSpace(vars["entryId"]),
		EntryType: shared.EntryType(strings.ToLower(strings.TrimSpace(vars["entryType"]))),
	}
	if !entry.EntryType.SupportsConversationView() {
		response.WriteErrorWithCode(w, http.StatusBadRequest, "invalid_entry_type", "Invalid entry type", nil)
		return shared.EntryRef{}, false
	}
	if middleware.GetClaims(r) == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return shared.EntryRef{}, false
	}
	return entry, true
}

func parseBefore(w http.ResponseWriter, r *http.Request) (*time.Time, bool) {
	raw := strings.TrimSpace(r.URL.Query().Get("before"))
	if raw == "" {
		return nil, true
	}
	at, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		response.WriteErrorWithCode(w, http.StatusBadRequest, "invalid_before", "before must be an RFC 3339 date", nil)
		return nil, false
	}
	return &at, true
}

func parseLimit(w http.ResponseWriter, r *http.Request) (int, bool) {
	raw := strings.TrimSpace(r.URL.Query().Get("limit"))
	if raw == "" {
		return 0, true
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 1 {
		response.WriteErrorWithCode(w, http.StatusBadRequest, "invalid_limit", "limit must be a positive number", nil)
		return 0, false
	}
	return limit, true
}

func personFrom(r *http.Request) shared.Person {
	claims := middleware.GetClaims(r)
	if claims == nil {
		return shared.Person{}
	}
	return shared.Person{UserID: claims.UserID, SystemAdmin: claims.Role == string(user.RoleAdmin)}
}

func writeDomainError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, conversation.ErrUnauthorized):
		response.WriteErrorWithCode(w, http.StatusForbidden, "forbidden", "You don't have access to this conversation", nil)
	case errors.Is(err, conversationnote.ErrNotFound):
		response.WriteErrorWithCode(w, http.StatusNotFound, "not_found", "Note not found", nil)
	case errors.Is(err, conversationnote.ErrNotAuthor):
		response.WriteErrorWithCode(w, http.StatusForbidden, "not_author", "Only the author can delete a note", nil)
	case errors.Is(err, conversationnote.ErrBodyRequired):
		response.WriteErrorWithCode(w, http.StatusBadRequest, "body_required", "The note is empty", nil)
	case errors.Is(err, conversationnote.ErrBodyTooLong):
		response.WriteErrorWithCode(w, http.StatusBadRequest, "body_too_long", "A note can have at most 4000 characters", nil)
	case errors.Is(err, conversationnote.ErrTooManyMentions):
		response.WriteErrorWithCode(w, http.StatusBadRequest, "too_many_mentions", "A note can mention at most 20 people", nil)
	default:
		response.WriteError(w, http.StatusInternalServerError, "Internal server error", nil)
	}
}
