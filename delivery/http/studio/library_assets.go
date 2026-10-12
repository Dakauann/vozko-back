package studiohttp

import (
	"context"
	"errors"
	"io"
	"log"
	"net/http"

	"github.com/gorilla/mux"

	"vozko/delivery/http/httpx"
	"vozko/delivery/http/response"
	"vozko/domain/media"
	"vozko/domain/studio/library"
	media_usecase "vozko/usecases/media"
)

type LibraryAssetImporter interface {
	Import(ctx context.Context, workspaceID, itemID, locale string, file io.Reader) (media.Media, error)
}

type LibraryAssetResponse struct {
	MediaID  string `json:"mediaId"`
	MediaURL string `json:"mediaUrl"`
}

func (h *Handler) WithLibraryAssets(assets LibraryAssetImporter) *Handler {
	h.libraryAssets = assets
	return h
}

// @Summary		Trazer uma figura da biblioteca de elementos para a biblioteca de mídia
// @Description	Figuras prontas da biblioteca de elementos (os itens de tipo asset, como os ícones e emojis em 3D) entram no projeto como imagens da biblioteca de mídia do workspace. O editor envia no corpo os bytes exatos do arquivo WebP do item, que o catálogo identifica pelo SHA-256; qualquer outro arquivo é recusado com 422 asset_mismatch e nada é guardado. O mesmo item no mesmo workspace devolve sempre a mesma mídia, sem duplicar; se ela foi apagada, uma nova é criada. locale escolhe o idioma do nome da mídia (pt, en, es ou de; padrão pt). Um item que não é figura responde 404 not_found; a biblioteca cheia responde 409 upload_limit.
// @Tags			Estúdio
// @Accept			image/webp
// @Produce		json
// @Param			itemId	path		string	true	"id do item na biblioteca de elementos"	example(3d-icon-rocket)
// @Param			locale	query		string	false	"idioma do nome da mídia"	Enums(pt, en, es, de)
// @Success		201		{object}	LibraryAssetResponse
// @Failure		404		{object}	response.ErrorResponse
// @Failure		409		{object}	response.ErrorResponse
// @Failure		422		{object}	response.ErrorResponse
// @Failure		503		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/studio/library/items/{itemId}/import [post]
func (h *Handler) ImportLibraryAsset(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := httpx.RequireWorkspace(w, r)
	if !ok {
		return
	}
	if h.libraryAssets == nil {
		response.WriteErrorWithCode(w, http.StatusServiceUnavailable, "unavailable", "A biblioteca de figuras não está disponível", nil)
		return
	}
	body := http.MaxBytesReader(w, r.Body, library.MaxAssetBytes+1)
	saved, err := h.libraryAssets.Import(r.Context(), workspaceID, mux.Vars(r)["itemId"], r.URL.Query().Get("locale"), body)
	if err != nil {
		writeLibraryAssetError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusCreated, LibraryAssetResponse{MediaID: saved.ID, MediaURL: saved.URL})
}

func writeLibraryAssetError(w http.ResponseWriter, err error) {
	var tooBig *http.MaxBytesError
	switch {
	case errors.Is(err, library.ErrUnknownAsset):
		response.WriteErrorWithCode(w, http.StatusNotFound, "not_found", "Essa figura não existe na biblioteca", nil)
	case errors.Is(err, library.ErrAssetMismatch), errors.As(err, &tooBig):
		response.WriteErrorWithCode(w, http.StatusUnprocessableEntity, "asset_mismatch", "O arquivo enviado não é a figura desse item", nil)
	case errors.Is(err, media_usecase.ErrUploadLimit):
		response.WriteErrorWithCode(w, http.StatusConflict, "upload_limit", "A biblioteca de mídia está cheia", nil)
	default:
		log.Printf("[studio] library picture import failed: %v", err)
		response.WriteError(w, http.StatusInternalServerError, "Failed to import the library picture", nil)
	}
}
