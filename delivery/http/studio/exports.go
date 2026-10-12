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
	exportFileField        = "file"
	exportDescriptionField = "description"
	multipartAllowance     = 1 << 20
	descriptionBytes       = studio.MaxExportDescriptionRunes * 4
)

type ExportSaver interface {
	Save(ctx context.Context, workspaceID, projectID, description string, file io.Reader) (media.Media, error)
}

type ExportedVideoResponse struct {
	MediaID  string `json:"mediaId"`
	MediaURL string `json:"mediaUrl"`
}

// @Summary		Guardar um arquivo exportado pelo editor
// @Description	O editor renderiza e codifica no navegador; o servidor não renderiza nada. Envie o arquivo no campo file (multipart) e, antes dele, um nome opcional no campo description (até 255 caracteres; vazio usa o nome do projeto). O servidor reconhece o formato pelos primeiros bytes, nunca pelo tipo declarado: projetos de vídeo exportam MP4, GIF e WebP animado; projetos de imagem exportam PDF e SVG. O SVG só é aceito com o vocabulário que o editor escreve (sem scripts, eventos, links externos ou textos; imagens só como data URI PNG ou JPEG). Tudo tem até 512 MB, conferidos enquanto o arquivo chega. O arquivo vai para a pasta de exportações do projeto e entra na biblioteca de mídia: MP4 como vídeo, GIF, WebP e SVG como imagem, PDF como documento. Nada é guardado quando o arquivo é recusado. Um arquivo que o editor não gera responde 422 invalid_export; um formato que o tipo do projeto não exporta responde 422 export_format; acima do limite responde 422 export_too_large.
// @Tags			Estúdio
// @Accept			multipart/form-data
// @Produce		json
// @Param			id			path		string	true	"id do projeto"
// @Param			description	formData	string	false	"nome da mídia na biblioteca"
// @Param			file		formData	file	true	"o arquivo exportado (MP4, GIF, WebP, PDF ou SVG)"
// @Success		201			{object}	ExportedVideoResponse
// @Failure		404			{object}	response.ErrorResponse
// @Failure		413			{object}	response.ErrorResponse
// @Failure		422			{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/studio/projects/{id}/exports [post]
func (h *Handler) SaveExport(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := httpx.RequireWorkspace(w, r)
	if !ok {
		return
	}
	description, file, err := exportParts(w, r)
	if err != nil {
		writeError(w, err, "Failed to read the exported file")
		return
	}
	saved, err := h.exports.Save(r.Context(), workspaceID, mux.Vars(r)["id"], description, file)
	if err != nil {
		writeError(w, err, "Failed to save the exported file")
		return
	}
	response.WriteSuccess(w, http.StatusCreated, ExportedVideoResponse{MediaID: saved.ID, MediaURL: saved.URL})
}

func exportParts(w http.ResponseWriter, r *http.Request) (string, io.Reader, error) {
	r.Body = http.MaxBytesReader(w, r.Body, studio.MaxExportBytes+multipartAllowance)
	parts, err := r.MultipartReader()
	if err != nil {
		return "", nil, studio.ErrInvalidExport
	}
	description := ""
	for {
		part, err := parts.NextPart()
		if err != nil {
			return "", nil, studio.ErrInvalidExport
		}
		switch part.FormName() {
		case exportFileField:
			return description, part, nil
		case exportDescriptionField:
			raw, err := io.ReadAll(io.LimitReader(part, descriptionBytes+1))
			if err != nil || len(raw) > descriptionBytes {
				return "", nil, studio.ErrInvalidExport
			}
			description = string(raw)
		}
	}
}
