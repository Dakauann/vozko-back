package instagram

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gorilla/mux"

	"vozko/delivery/http/response"
	"vozko/infra/http/middleware"
	iguc "vozko/usecases/instagram"
)

// ListComments returns one page of comments on a post, with replies expanded.
//
// Graph caps this at 50 per query and cannot filter by timestamp, so the page
// size is bounded upstream and paging is cursor-based.
//
//	@Summary		Listar comentários de uma publicação
//	@Tags			Instagram
//	@Produce		json
//	@Success		200	{object}	PageResponse[CommentResponse]
//	@Security		BearerAuth
//	@Router			/instagram/accounts/{id}/media/{mediaId}/comments [get]
func (h *Handler) ListComments(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))

	page, err := h.listComments.Execute(r.Context(), iguc.ListCommentsInput{
		WorkspaceID: middleware.GetWorkspaceID(r),
		AccountID:   vars["id"],
		IGMediaID:   vars["mediaId"],
		Limit:       limit,
		After:       strings.TrimSpace(r.URL.Query().Get("after")),
	})
	if err != nil {
		writeDomainError(w, err, "Failed to list Instagram comments")
		return
	}

	items := make([]CommentResponse, 0, len(page.Items))
	for _, c := range page.Items {
		items = append(items, toCommentResponse(c))
	}
	response.WriteSuccess(w, http.StatusOK, PageResponse[CommentResponse]{
		Items:      items,
		NextCursor: page.NextCursor,
		HasNext:    page.HasNext,
	})
}

// ReplyComment posts a public threaded reply.
//
//	@Summary		Responder comentário do Instagram
//	@Tags			Instagram
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Router			/instagram/accounts/{id}/comments/{commentId}/replies [post]
func (h *Handler) ReplyComment(w http.ResponseWriter, r *http.Request) {
	var req ReplyCommentRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Message) == "" {
		response.WriteError(w, http.StatusBadRequest, "Reply message is required", nil)
		return
	}

	vars := mux.Vars(r)
	newID, err := h.replyComment.Execute(r.Context(),
		middleware.GetWorkspaceID(r), vars["id"], vars["commentId"], req.Message)
	if err != nil {
		writeDomainError(w, err, "Failed to reply to Instagram comment")
		return
	}
	response.WriteSuccess(w, http.StatusCreated, map[string]string{"id": newID})
}

// HideComment hides or unhides a comment.
//
// Hiding is the moderation action that works on anyone's comment, because it only
// needs the media owner's token. Deletion needs the comment author's token.
//
//	@Summary		Ocultar/exibir comentário do Instagram
//	@Tags			Instagram
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Router			/instagram/accounts/{id}/comments/{commentId}/hide [post]
func (h *Handler) HideComment(w http.ResponseWriter, r *http.Request) {
	var req HideCommentRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	vars := mux.Vars(r)

	if err := h.moderate.SetHidden(r.Context(),
		middleware.GetWorkspaceID(r), vars["id"], vars["commentId"], req.Hidden); err != nil {
		writeDomainError(w, err, "Failed to moderate Instagram comment")
		return
	}
	response.WriteSuccess(w, http.StatusOK, map[string]bool{"hidden": req.Hidden})
}

// DeleteComment deletes a comment we authored.
//
//	@Summary		Excluir comentário do Instagram
//	@Description	Só é possível excluir comentários criados pela própria conta; para os demais, use ocultar.
//	@Tags			Instagram
//	@Security		BearerAuth
//	@Router			/instagram/accounts/{id}/comments/{commentId} [delete]
func (h *Handler) DeleteComment(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	if err := h.moderate.Delete(r.Context(),
		middleware.GetWorkspaceID(r), vars["id"], vars["commentId"]); err != nil {
		writeDomainError(w, err, "Failed to delete Instagram comment")
		return
	}
	response.WriteSuccess(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// PrivateReply DMs the author of a public comment.
//
// Instagram permits exactly one per comment, ever, and only within 7 days. The
// allowance is claimed before the upstream call, so a duplicate request is
// rejected with a specific code rather than silently consuming it.
//
//	@Summary		Responder comentário por mensagem privada
//	@Tags			Instagram
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Router			/instagram/accounts/{id}/comments/{commentId}/private-reply [post]
func (h *Handler) PrivateReply(w http.ResponseWriter, r *http.Request) {
	var req PrivateReplyRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Text) == "" {
		response.WriteError(w, http.StatusBadRequest, "Private reply text is required", nil)
		return
	}

	vars := mux.Vars(r)
	if err := h.privateReply.Execute(r.Context(),
		middleware.GetWorkspaceID(r), vars["id"], vars["commentId"], req.Text); err != nil {
		writeDomainError(w, err, "Failed to send Instagram private reply")
		return
	}
	response.WriteSuccess(w, http.StatusOK, map[string]string{"status": "sent"})
}
