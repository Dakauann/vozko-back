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

// @Summary		Listar comentários de uma publicação
// @Tags			Instagram
// @Param			id	path	string	true	"ID da conta do Instagram"
// @Param			mediaId	path	string	true	"ID da publicação"
// @Produce		json
// @Success		200	{object}	PageResponse[CommentResponse]
// @Security		BearerAuth
// @Router			/instagram/accounts/{id}/media/{mediaId}/comments [get]
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

// @Summary		Responder comentário do Instagram
// @Tags			Instagram
// @Param			id	path	string	true	"ID da conta do Instagram"
// @Param			commentId	path	string	true	"ID do comentário"
// @Accept			json
// @Produce		json
// @Success		201	{object}	map[string]string
// @Security		BearerAuth
// @Router			/instagram/accounts/{id}/comments/{commentId}/replies [post]
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

// @Summary		Ocultar/exibir comentário do Instagram
// @Tags			Instagram
// @Param			id	path	string	true	"ID da conta do Instagram"
// @Param			commentId	path	string	true	"ID do comentário"
// @Accept			json
// @Produce		json
// @Success		200	{object}	map[string]bool
// @Security		BearerAuth
// @Router			/instagram/accounts/{id}/comments/{commentId}/hide [post]
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

// @Summary		Excluir comentário do Instagram
// @Description	Só é possível excluir comentários criados pela própria conta; para os demais, use ocultar.
// @Tags			Instagram
// @Param			id	path	string	true	"ID da conta do Instagram"
// @Param			commentId	path	string	true	"ID do comentário"
// @Produce		json
// @Success		200	{object}	map[string]string
// @Security		BearerAuth
// @Router			/instagram/accounts/{id}/comments/{commentId} [delete]
func (h *Handler) DeleteComment(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	if err := h.moderate.Delete(r.Context(),
		middleware.GetWorkspaceID(r), vars["id"], vars["commentId"]); err != nil {
		writeDomainError(w, err, "Failed to delete Instagram comment")
		return
	}
	response.WriteSuccess(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// @Summary		Responder comentário por mensagem privada
// @Tags			Instagram
// @Param			id	path	string	true	"ID da conta do Instagram"
// @Param			commentId	path	string	true	"ID do comentário"
// @Accept			json
// @Produce		json
// @Success		200	{object}	map[string]string
// @Security		BearerAuth
// @Router			/instagram/accounts/{id}/comments/{commentId}/private-reply [post]
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
