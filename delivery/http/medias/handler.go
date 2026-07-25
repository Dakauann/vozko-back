package medias

import (
	"errors"
	"io"
	"net/http"
	"path/filepath"

	"github.com/google/uuid"
	"github.com/gorilla/mux"

	"vozko/delivery/http/response"
	mediadomain "vozko/domain/media"
	"vozko/infra/http/middleware"
)

type MediasHandler struct {
	uploadMediaUsecase     mediadomain.UploadMediaUseCase
	listMediaUseCase       mediadomain.ListMediaUseCase
	getMediaUseCase        mediadomain.GetMediaUseCase
	deleteHoldMusicUseCase mediadomain.DeleteHoldMusicUseCase
}

func NewMediasHandler(uploadMediaUsecase mediadomain.UploadMediaUseCase, listMediaUseCase mediadomain.ListMediaUseCase, getMediaUseCase mediadomain.GetMediaUseCase, deleteHoldMusicUseCase mediadomain.DeleteHoldMusicUseCase) *MediasHandler {
	return &MediasHandler{
		uploadMediaUsecase:     uploadMediaUsecase,
		listMediaUseCase:       listMediaUseCase,
		getMediaUseCase:        getMediaUseCase,
		deleteHoldMusicUseCase: deleteHoldMusicUseCase,
	}
}

// @Summary		Listar mídias
// @Description	Retorna as mídias do workspace (imagens, vídeos, documentos, áudios e músicas de espera personalizadas) usadas em campanhas, workflows e configurações.
// @Tags			Mídias
// @Produce		json
// @Success		200	{array}		MediaResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/medias [get]
func (h *MediasHandler) ListMedias(w http.ResponseWriter, r *http.Request) {
	workspaceID := middleware.GetWorkspaceID(r)
	images, err := h.listMediaUseCase.ListMedia(workspaceID)
	if err != nil {
		http.Error(w, "Unable to list images", http.StatusInternalServerError)
		return
	}

	response.WriteSuccess(w, http.StatusOK, toMediaResponses(images))
}

// @Summary		Enviar uma mídia
// @Description	Envia um arquivo de mídia para o workspace via multipart. Músicas de espera personalizadas exigem plano compatível e respeitam o limite de faixas da conta.
// @Tags			Mídias
// @Accept			multipart/form-data
// @Produce		json
// @Param			mediaType	formData	string	true	"Tipo da mídia (ex.: image, video, audio, hold_music)"
// @Param			media		formData	file	true	"Arquivo da mídia"
// @Param			description	formData	string	true	"Descrição da mídia"
// @Success		201	{object}	MediaResponse
// @Failure		400	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		409	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/medias [post]
func (h *MediasHandler) UploadMedia(w http.ResponseWriter, r *http.Request) {
	var mediaData []byte
	var mediaName string

	err := r.ParseMultipartForm(25 << 20)
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, "Unable to parse form", nil)
		return
	}

	mediaType := r.FormValue("mediaType")
	if mediaType == "" {
		response.WriteError(w, http.StatusBadRequest, "Unable to get media type", nil)
		return
	}

	file, header, err := r.FormFile("media")
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, "Unable to get media file", nil)
		return
	}

	mediaDescription := r.FormValue("description")
	if mediaDescription == "" {
		response.WriteError(w, http.StatusBadRequest, "Unable to get media description", nil)
		return
	}

	defer file.Close()

	mediaData, err = io.ReadAll(file)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Unable to read media file", nil)
		return
	}

	extension := ""
	if header != nil {
		extension = filepath.Ext(header.Filename)
	}

	mediaName = uuid.New().String() + extension

	workspaceID := middleware.GetWorkspaceID(r)

	uploaded, err := h.uploadMediaUsecase.UploadMedia(workspaceID, mediaData, mediaName, mediadomain.MediaType(mediaType), mediaDescription)
	if err != nil {
		switch {
		case errors.Is(err, mediadomain.ErrHoldMusicNotIncluded):
			response.WriteError(w, http.StatusForbidden, "O seu plano não inclui músicas de espera personalizadas", nil)
		case errors.Is(err, mediadomain.ErrHoldMusicQuotaReached):
			response.WriteError(w, http.StatusConflict, "Limite de músicas de espera atingido; exclua uma faixa para liberar espaço", nil)
		default:
			response.WriteError(w, http.StatusInternalServerError, "Unable to upload media: "+err.Error(), nil)
		}
		return
	}

	response.WriteSuccess(w, http.StatusCreated, toMediaResponse(uploaded))
}

// @Summary		Excluir uma música de espera
// @Description	Remove uma música de espera personalizada do workspace, liberando uma vaga da cota do plano. Apenas faixas do tipo hold_music podem ser excluídas por esta rota.
// @Tags			Mídias
// @Produce		json
// @Param			id	path	string	true	"ID da mídia"
// @Success		200	{object}	MediaDeletedResponse
// @Failure		400	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Failure		501	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/medias/{id} [delete]
func (h *MediasHandler) DeleteHoldMusic(w http.ResponseWriter, r *http.Request) {
	if h.deleteHoldMusicUseCase == nil {
		response.WriteError(w, http.StatusNotImplemented, "Hold music management is not enabled on this server", nil)
		return
	}
	mediaID := mux.Vars(r)["id"]
	if mediaID == "" {
		response.WriteError(w, http.StatusBadRequest, "Media ID is required", nil)
		return
	}
	workspaceID := middleware.GetWorkspaceID(r)

	err := h.deleteHoldMusicUseCase.DeleteHoldMusic(r.Context(), workspaceID, mediaID)
	switch {
	case err == nil:
		response.WriteSuccess(w, http.StatusOK, MediaDeletedResponse{Deleted: mediaID})
	case errors.Is(err, mediadomain.ErrMediaNotFound):
		response.WriteError(w, http.StatusNotFound, "Media not found", nil)
	case errors.Is(err, mediadomain.ErrNotHoldMusic):
		response.WriteError(w, http.StatusBadRequest, "Only hold music tracks can be deleted here", nil)
	default:
		response.WriteError(w, http.StatusInternalServerError, "Unable to delete media", nil)
	}
}

// @Summary		Obter uma mídia
// @Description	Retorna os detalhes de uma mídia do workspace pelo seu identificador.
// @Tags			Mídias
// @Produce		json
// @Param			id	path	string	true	"ID da mídia"
// @Success		200	{object}	MediaResponse
// @Failure		400	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/medias/{id} [get]
func (h *MediasHandler) GetMedia(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	mediaID := vars["id"]
	if mediaID == "" {
		response.WriteError(w, http.StatusBadRequest, "Media ID is required", nil)
		return
	}

	media, err := h.getMediaUseCase.GetMedia(mediaID)
	if err != nil {
		response.WriteError(w, http.StatusNotFound, "Media not found", nil)
		return
	}

	response.WriteSuccess(w, http.StatusOK, toMediaResponsePtr(media))
}
