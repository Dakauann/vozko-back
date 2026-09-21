package instagram

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gorilla/mux"

	"vozko/delivery/http/response"
	igdomain "vozko/domain/instagram"
	"vozko/infra/http/middleware"
	iguc "vozko/usecases/instagram"
)

// @Summary		Listar publicações do Instagram
// @Description	Retorna as publicações da conta com paginação por cursor.
// @Tags			Instagram
// @Param			id	path	string	true	"ID da conta do Instagram"
// @Produce		json
// @Success		200	{object}	PageResponse[MediaResponse]
// @Security		BearerAuth
// @Router			/instagram/accounts/{id}/media [get]
func (h *Handler) ListMedia(w http.ResponseWriter, r *http.Request) {
	accountID := mux.Vars(r)["id"]
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))

	page, err := h.listMedia.Execute(r.Context(), iguc.ListMediaInput{
		WorkspaceID: middleware.GetWorkspaceID(r),
		AccountID:   accountID,
		Limit:       limit,
		After:       strings.TrimSpace(r.URL.Query().Get("after")),
	})
	if err != nil {
		writeDomainError(w, err, "Failed to list Instagram posts")
		return
	}

	items := make([]MediaResponse, 0, len(page.Items))
	for _, m := range page.Items {
		items = append(items, h.toMediaResponse(accountID, m))
	}

	response.WriteSuccess(w, http.StatusOK, PageResponse[MediaResponse]{
		Items:      items,
		NextCursor: page.NextCursor,
		HasNext:    page.HasNext,
	})
}

// @Summary		Obter publicação do Instagram
// @Tags			Instagram
// @Param			id	path	string	true	"ID da conta do Instagram"
// @Param			mediaId	path	string	true	"ID da publicação"
// @Produce		json
// @Success		200	{object}	MediaResponse
// @Security		BearerAuth
// @Router			/instagram/accounts/{id}/media/{mediaId} [get]
func (h *Handler) GetMedia(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	media, err := h.getMedia.Execute(r.Context(), middleware.GetWorkspaceID(r), vars["id"], vars["mediaId"])
	if err != nil {
		writeDomainError(w, err, "Failed to load Instagram post")
		return
	}
	response.WriteSuccess(w, http.StatusOK, h.toMediaResponse(vars["id"], media))
}

// @Summary		Proxy de mídia do Instagram
// @Tags			Instagram
// @Param			id	path	string	true	"ID da conta do Instagram"
// @Param			mediaId	path	string	true	"ID da publicação"
// @Success		200
// @Security		BearerAuth
// @Router			/instagram/accounts/{id}/media/{mediaId}/asset [get]
func (h *Handler) ProxyMedia(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	thumb := r.URL.Query().Get("thumb") == "1"

	data, contentType, err := h.proxyMedia.Execute(r.Context(),
		middleware.GetWorkspaceID(r), vars["id"], vars["mediaId"], thumb)
	if err != nil {
		writeDomainError(w, err, "Failed to load Instagram media")
		return
	}

	if contentType == "" {
		contentType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.Header().Set("Cache-Control", "private, max-age=300")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// @Summary		Publicar no Instagram
// @Description	Cria o container, aguarda o processamento e publica.
// @Tags			Instagram
// @Param			id	path	string	true	"ID da conta do Instagram"
// @Accept			json
// @Produce		json
// @Success		201	{object}	MediaResponse
// @Security		BearerAuth
// @Router			/instagram/accounts/{id}/media [post]
func (h *Handler) CreateMedia(w http.ResponseWriter, r *http.Request) {
	var req CreateMediaRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	accountID := mux.Vars(r)["id"]

	media, err := h.createMedia.Execute(r.Context(), iguc.CreateMediaInput{
		WorkspaceID: middleware.GetWorkspaceID(r),
		AccountID:   accountID,
		ImageURL:    strings.TrimSpace(req.ImageURL),
		VideoURL:    strings.TrimSpace(req.VideoURL),
		Caption:     req.Caption,
		MediaType:   strings.TrimSpace(req.MediaType),
	})
	if err != nil {
		writeDomainError(w, err, "Failed to publish Instagram post")
		return
	}
	response.WriteSuccess(w, http.StatusCreated, h.toMediaResponse(accountID, media))
}

// @Summary		Atualizar publicação do Instagram
// @Description	Somente habilitar/desabilitar comentários. A legenda não pode ser editada pela API.
// @Tags			Instagram
// @Param			id	path	string	true	"ID da conta do Instagram"
// @Param			mediaId	path	string	true	"ID da publicação"
// @Accept			json
// @Produce		json
// @Success		200	{object}	map[string]bool
// @Security		BearerAuth
// @Router			/instagram/accounts/{id}/media/{mediaId} [patch]
func (h *Handler) UpdateMedia(w http.ResponseWriter, r *http.Request) {
	var req UpdateMediaRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.CommentEnabled == nil {
		response.WriteErrorWithCode(w, http.StatusBadRequest, "caption_immutable",
			"Instagram only supports enabling or disabling comments on a published post; captions cannot be edited via the API", nil)
		return
	}

	vars := mux.Vars(r)
	if err := h.setCommentEnabled.Execute(r.Context(),
		middleware.GetWorkspaceID(r), vars["id"], vars["mediaId"], *req.CommentEnabled); err != nil {
		writeDomainError(w, err, "Failed to update Instagram post")
		return
	}
	response.WriteSuccess(w, http.StatusOK, map[string]bool{"commentEnabled": *req.CommentEnabled})
}

func (h *Handler) toMediaResponse(accountID string, m *igdomain.RemoteMedia) MediaResponse {
	if m == nil {
		return MediaResponse{}
	}
	out := MediaResponse{
		ID:               m.IGMediaID,
		MediaType:        string(m.MediaType),
		MediaProductType: string(m.MediaProductType),
		IsReel:           m.MediaProductType == igdomain.MediaProductReels,
		IsCarousel:       m.MediaType == igdomain.MediaTypeCarousel,
		Caption:          m.Caption,
		Permalink:        m.Permalink,
		Shortcode:        m.Shortcode,
		Timestamp:        m.Timestamp,
		LikeCount:        m.LikeCount,
		CommentsCount:    m.CommentsCount,
		IsCommentEnabled: m.IsCommentEnabled,
		HasAsset:         m.MediaURL != "",
	}

	if m.MediaURL != "" {
		out.MediaURL = proxyPath(accountID, m.IGMediaID, false)
	}
	if m.ThumbnailURL != "" {
		out.ThumbnailURL = proxyPath(accountID, m.IGMediaID, true)
	} else if m.MediaURL != "" && m.MediaType != igdomain.MediaTypeVideo {
		out.ThumbnailURL = out.MediaURL
	}

	for _, child := range m.Children {
		out.Children = append(out.Children, h.toMediaResponse(accountID, child))
	}
	return out
}

func proxyPath(accountID, mediaID string, thumb bool) string {
	base := fmt.Sprintf("/instagram/accounts/%s/media/%s/asset", accountID, mediaID)
	if thumb {
		return base + "?thumb=1"
	}
	return base
}

// @Summary		Foto de perfil da conta
// @Description	Faz proxy da foto de perfil, cuja URL de CDN expira.
// @Tags			Instagram
// @Param			id	path	string	true	"ID da conta do Instagram"
// @Produce		image/jpeg
// @Success		200
// @Security		BearerAuth
// @Router			/instagram/accounts/{id}/avatar [get]
func (h *Handler) ProxyAvatar(w http.ResponseWriter, r *http.Request) {
	data, contentType, err := h.proxyAvatar.Execute(r.Context(),
		middleware.GetWorkspaceID(r), mux.Vars(r)["id"])
	if err != nil {
		if errors.Is(err, iguc.ErrNoAvatar) {
			http.NotFound(w, r)
			return
		}
		writeDomainError(w, err, "Failed to load Instagram avatar")
		return
	}

	if contentType == "" {
		contentType = "image/jpeg"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.Header().Set("Cache-Control", "private, max-age=3600")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}
