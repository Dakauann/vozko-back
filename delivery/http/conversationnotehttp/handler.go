package conversationnotehttp

import (
	"encoding/json"
	"errors"
	"io"
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

const (
	uploadOverhead  = 1 << 20
	multipartMemory = 1 << 20
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
// @Param			entryType	path		string	true	"Canal da conversa: whatsapp, unofficial_whatsapp, instagram, facebook, telegram, webchat ou email"
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
// @Description	Grava uma nota interna na conversa, visível só para a equipe que pode ver a conversa. O texto tem até 4000 caracteres (sem contar espaços nas pontas) e só pode ficar vazio quando a nota leva imagens. `mentions` traz até 20 IDs de usuários (repetidos contam uma vez); IDs que não são membros do workspace são descartados sem erro e não ficam gravados. `image_ids` traz até 4 imagens enviadas antes por `POST /conversations/{entryType}/{entryId}/notes/images`, na ordem em que aparecem (repetidas contam uma vez); cada uma precisa ser uma imagem desta conversa. Cada mencionado que pode ver a conversa recebe uma notificação na categoria `conversations`, com o autor e o contato no título e um link para a conversa (nunca o texto nem as imagens da nota); quem não pode ver a conversa não é notificado nem fica sabendo da nota. Os inscritos na conversa recebem `conversation:note` pelo WebSocket de conversas, já com as imagens.
// @Tags			Notas Internas
// @Accept			json
// @Produce		json
// @Param			entryType	path		string				true	"Canal da conversa"
// @Param			entryId		path		string				true	"ID da conversa"
// @Param			request		body		CreateNoteRequest	true	"Texto, menções e imagens"
// @Success		201	{object}	NoteEnvelope
// @Failure		400	{object}	response.ErrorResponse	"invalid_entry_type, body_required, body_too_long, too_many_mentions, too_many_images, image_not_found, image_type ou image_too_large"
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
			"body":      "string (até 4000 caracteres; obrigatório sem imagens)",
			"mentions":  "string[] (opcional, até 20 IDs de usuários)",
			"image_ids": "string[] (opcional, até 4 IDs de imagens da nota)",
		})
		return
	}

	note, err := h.notes.Create(r.Context(), personFrom(r), conversationnote.Draft{
		WorkspaceID: middleware.GetWorkspaceID(r),
		Entry:       entry,
		Body:        req.Body,
		Mentions:    req.Mentions,
		ImageIDs:    req.ImageIDs,
	})
	if err != nil {
		writeDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusCreated, NoteEnvelope{Note: conversationnoteview.Of(note)})
}

// @Summary		Enviar uma imagem para uma nota interna
// @Description	Recebe uma imagem (multipart/form-data, campo `image`) para anexar depois a uma nota interna com `image_ids`. O tipo é lido do conteúdo do arquivo, nunca do nome nem do cabeçalho: só JPEG, PNG, WebP e GIF, até 10 MB cada. A imagem fica guardada com as mídias da conversa, com largura, altura e thumbhash quando o arquivo permite medir. Ela nunca vai para o contato, nem para a IA, a análise, os fluxos, as exportações ou o chat do site, e some da conversa junto com a nota quando a nota é apagada; os arquivos saem quando a conversa é removida. Vale o mesmo acesso de ler as notas (departamento, responsável, `view_others` e `conversations:read`).
// @Tags			Notas Internas
// @Accept			mpfd
// @Produce		json
// @Param			entryType	path		string	true	"Canal da conversa"
// @Param			entryId		path		string	true	"ID da conversa"
// @Param			image		formData	file	true	"Imagem JPEG, PNG, WebP ou GIF (até 10 MB)"
// @Success		201	{object}	NoteImageEnvelope
// @Failure		400	{object}	response.ErrorResponse	"invalid_entry_type, image_required ou image_type"
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse	"forbidden: sem acesso a esta conversa"
// @Failure		413	{object}	response.ErrorResponse	"image_too_large"
// @Failure		503	{object}	response.ErrorResponse	"storage_unavailable"
// @Security		BearerAuth
// @Router			/conversations/{entryType}/{entryId}/notes/images [post]
func (h *Handler) AttachImage(w http.ResponseWriter, r *http.Request) {
	entry, ok := entryFromPath(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, conversationnote.MaxImageBytes+uploadOverhead)
	if err := r.ParseMultipartForm(multipartMemory); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeDomainError(w, conversationnote.ErrImageTooLarge)
			return
		}
		writeDomainError(w, conversationnote.ErrImageRequired)
		return
	}
	defer r.MultipartForm.RemoveAll()
	file, header, err := r.FormFile("image")
	if err != nil {
		writeDomainError(w, conversationnote.ErrImageRequired)
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, conversationnote.MaxImageBytes+1))
	if err != nil {
		writeDomainError(w, conversationnote.ErrImageRequired)
		return
	}

	img, err := h.notes.AttachImage(r.Context(), personFrom(r), conversationnote.ImageUpload{
		WorkspaceID: middleware.GetWorkspaceID(r),
		Entry:       entry,
		FileName:    header.Filename,
		Data:        data,
	})
	if err != nil {
		writeDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusCreated, NoteImageEnvelope{Image: conversationnoteview.ImageOf(img)})
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
	case errors.Is(err, conversationnote.ErrTooManyImages):
		response.WriteErrorWithCode(w, http.StatusBadRequest, "too_many_images", "Uma nota leva no máximo 4 imagens", nil)
	case errors.Is(err, conversationnote.ErrImageRequired):
		response.WriteErrorWithCode(w, http.StatusBadRequest, "image_required", "Envie a imagem no campo image", nil)
	case errors.Is(err, conversationnote.ErrImageType):
		response.WriteErrorWithCode(w, http.StatusBadRequest, "image_type", "Só imagens JPEG, PNG, WebP ou GIF entram em uma nota", nil)
	case errors.Is(err, conversationnote.ErrImageTooLarge):
		response.WriteErrorWithCode(w, http.StatusRequestEntityTooLarge, "image_too_large", "Cada imagem pode ter até 10 MB", nil)
	case errors.Is(err, conversationnote.ErrImageNotFound):
		response.WriteErrorWithCode(w, http.StatusBadRequest, "image_not_found", "A imagem não foi encontrada nesta conversa", nil)
	case errors.Is(err, conversationnote.ErrStorageMissing):
		response.WriteErrorWithCode(w, http.StatusServiceUnavailable, "storage_unavailable", "O armazenamento de imagens não está disponível", nil)
	default:
		response.WriteError(w, http.StatusInternalServerError, "Internal server error", nil)
	}
}
