package advertisinghttp

import (
	"net/http"
	"time"

	"github.com/gorilla/mux"

	"vozko/delivery/http/response"
	"vozko/domain/advertising"
)

type AdCommentResponse struct {
	ID         string     `json:"id"`
	Message    string     `json:"message"`
	AuthorName string     `json:"authorName"`
	CreatedAt  *time.Time `json:"createdAt"`
	LikeCount  int        `json:"likeCount"`
	ReplyCount int        `json:"replyCount"`
}

func presentComments(comments []advertising.AdComment) []AdCommentResponse {
	out := make([]AdCommentResponse, 0, len(comments))
	for _, c := range comments {
		out = append(out, AdCommentResponse{ID: c.ID, Message: c.Message, AuthorName: c.AuthorName, CreatedAt: c.CreatedAt, LikeCount: c.LikeCount, ReplyCount: c.ReplyCount})
	}
	return out
}

// @Summary		Comentários de um anúncio
// @Description	Os 100 comentários mais recentes da publicação que a Meta criou para o anúncio, no Facebook (com o token da Página) ou no Instagram. Só anúncios têm comentários; um anúncio ainda sem publicação na plataforma pedida volta 404 ad_post_missing.
// @Tags			Anúncios
// @Produce		json
// @Param			metaId		path		string	true	"ID do anúncio na Meta"
// @Param			platform	query		string	false	"facebook (padrão) ou instagram"
// @Success		200			{array}		AdCommentResponse
// @Failure		400			{object}	response.ErrorResponse
// @Failure		404			{object}	response.ErrorResponse
// @Failure		409			{object}	response.ErrorResponse
// @Failure		422			{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/ads/objects/{metaId}/comments [get]
func (h *Handler) AdComments(w http.ResponseWriter, r *http.Request) {
	comments, err := h.d.Comments.List(r.Context(), workspaceOf(r), mux.Vars(r)["metaId"], r.URL.Query().Get("platform"))
	if err != nil {
		writeError(w, err, "Failed to load the ad comments")
		return
	}
	response.WriteSuccess(w, http.StatusOK, presentComments(comments))
}
