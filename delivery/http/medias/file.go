package medias

import (
	"errors"
	"log"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"

	"vozko/delivery/http/response"
	mediadomain "vozko/domain/media"
	"vozko/infra/http/middleware"
)

var fileExtensions = map[string]string{
	"image/jpeg":      ".jpg",
	"image/png":       ".png",
	"image/webp":      ".webp",
	"image/gif":       ".gif",
	"video/mp4":       ".mp4",
	"video/quicktime": ".mov",
	"audio/mpeg":      ".mp3",
	"audio/ogg":       ".ogg",
	"application/pdf": ".pdf",
}

// @Summary		Baixar o arquivo de uma mídia
// @Description	Devolve o conteúdo de uma mídia do workspace como anexo (Content-Disposition attachment), com o Content-Type original. Serve para baixar imagens geradas com IA e outros arquivos da biblioteca sem depender do domínio público do armazenamento. Planilhas enviadas para importação de leads não são servidas e respondem 404.
// @Tags			Mídias
// @Produce		octet-stream
// @Param			id	path	string	true	"ID da mídia"
// @Success		200	{file}		binary
// @Failure		404	{object}	response.ErrorResponse
// @Failure		413	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/medias/{id}/file [get]
func (h *MediasHandler) DownloadMedia(w http.ResponseWriter, r *http.Request) {
	mediaID := mux.Vars(r)["id"]
	content, err := h.readMediaUseCase.Read(r.Context(), middleware.GetWorkspaceID(r), mediaID)
	switch {
	case errors.Is(err, mediadomain.ErrMediaNotFound):
		response.WriteErrorWithCode(w, http.StatusNotFound, "not_found", "Mídia não encontrada", nil)
		return
	case errors.Is(err, mediadomain.ErrMediaTooLarge):
		response.WriteErrorWithCode(w, http.StatusRequestEntityTooLarge, "too_large", "Arquivo grande demais para baixar", nil)
		return
	case err != nil:
		log.Printf("[medias] download %s: %v", mediaID, err)
		response.WriteError(w, http.StatusInternalServerError, "Não foi possível baixar a mídia", nil)
		return
	}
	contentType := content.ContentType
	if contentType == "" {
		contentType = http.DetectContentType(content.Data)
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Length", strconv.Itoa(len(content.Data)))
	w.Header().Set("Content-Disposition", `attachment; filename="`+content.Media.ID+fileExtensions[contentType]+`"`)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(content.Data)
}
