package studiohttp

import (
	"context"
	"io"
	"net/http"

	"github.com/gorilla/mux"

	"vozko/delivery/http/httpx"
	"vozko/delivery/http/response"
	"vozko/domain/media"
	"vozko/domain/studio"
)

const (
	exportFormField    = "video"
	multipartAllowance = 1 << 20
)

type ExportSaver interface {
	Save(ctx context.Context, workspaceID, projectID string, video io.Reader) (media.Media, error)
}

type ExportedVideoResponse struct {
	MediaID  string `json:"mediaId"`
	MediaURL string `json:"mediaUrl"`
}

// @Summary		Guardar um vídeo exportado
// @Description	O editor renderiza e codifica o vídeo no navegador; o servidor não renderiza nada. Envie o MP4 no campo video (multipart). O servidor confere que o arquivo é um MP4 e que tem até 95 MB enquanto recebe, e só então guarda na pasta de exportações do projeto e coloca o vídeo na biblioteca de mídia com o nome do projeto. Nada é guardado quando o arquivo é recusado. Só projetos de vídeo exportam (422 not_video); um arquivo que não é MP4 responde 422 invalid_export; acima do limite responde 422 export_too_large.
// @Tags			Estúdio
// @Accept			multipart/form-data
// @Produce		json
// @Param			id		path		string	true	"id do projeto"
// @Param			video	formData	file	true	"o vídeo exportado em MP4"
// @Success		201		{object}	ExportedVideoResponse
// @Failure		404		{object}	response.ErrorResponse
// @Failure		413		{object}	response.ErrorResponse
// @Failure		422		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/studio/projects/{id}/exports [post]
func (h *Handler) SaveExport(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := httpx.RequireWorkspace(w, r)
	if !ok {
		return
	}
	video, err := exportPart(w, r)
	if err != nil {
		writeError(w, err, "Failed to read the exported video")
		return
	}
	saved, err := h.exports.Save(r.Context(), workspaceID, mux.Vars(r)["id"], video)
	if err != nil {
		writeError(w, err, "Failed to save the exported video")
		return
	}
	response.WriteSuccess(w, http.StatusCreated, ExportedVideoResponse{MediaID: saved.ID, MediaURL: saved.URL})
}

func exportPart(w http.ResponseWriter, r *http.Request) (io.Reader, error) {
	r.Body = http.MaxBytesReader(w, r.Body, studio.MaxExportBytes+multipartAllowance)
	parts, err := r.MultipartReader()
	if err != nil {
		return nil, studio.ErrInvalidExport
	}
	for {
		part, err := parts.NextPart()
		if err != nil {
			return nil, studio.ErrInvalidExport
		}
		if part.FormName() == exportFormField {
			return part, nil
		}
	}
}
