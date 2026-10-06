package mediagenhttp

import (
	"context"
	"net/http"
	"time"

	"github.com/gorilla/mux"

	"vozko/delivery/http/httpx"
	"vozko/delivery/http/response"
	"vozko/domain/mediagen"
	"vozko/infra/http/middleware"
)

type Service interface {
	Models(ctx context.Context, kind mediagen.Kind) ([]mediagen.Model, error)
	Request(ctx context.Context, req mediagen.Request, requestedBy string) (*mediagen.Job, error)
	Get(ctx context.Context, workspaceID, id string) (*mediagen.Job, error)
}

type Handler struct {
	svc Service
}

func NewHandler(svc Service) *Handler {
	return &Handler{svc: svc}
}

type GenerateRequest struct {
	Kind              string             `json:"kind" enums:"image,music,voice,video,cutout,captions,denoise" example:"music"`
	Model             string             `json:"model,omitempty" example:"google/lyria-3-clip-preview"`
	Prompt            string             `json:"prompt,omitempty" example:"Samba leve e acústico para o anúncio de uma cafeteria"`
	Aspect            string             `json:"aspect,omitempty" enums:"square,portrait,story"`
	ReferenceMediaIDs []string           `json:"referenceMediaIds,omitempty" maxItems:"16"`
	Voice             string             `json:"voice,omitempty" example:"alloy"`
	Video             *mediagen.Timeline `json:"video,omitempty"`
	SourceMediaID     string             `json:"sourceMediaId,omitempty"`
}

func (b GenerateRequest) request(workspaceID string) mediagen.Request {
	req := mediagen.Request{
		WorkspaceID: workspaceID, Kind: mediagen.Kind(b.Kind), Model: b.Model, Prompt: b.Prompt, Aspect: mediagen.Aspect(b.Aspect),
		ReferenceMediaIDs: b.ReferenceMediaIDs, Voice: b.Voice, SourceMediaID: b.SourceMediaID,
	}
	if b.Video != nil {
		req.Video = *b.Video
	}
	return req
}

type ModelResponse struct {
	ID      string `json:"id" example:"google/lyria-3-clip-preview"`
	Name    string `json:"name" example:"Google: Lyria 3 Clip Preview"`
	Default bool   `json:"default"`
}

type JobResponse struct {
	ID                string             `json:"id"`
	Kind              string             `json:"kind" enums:"image,music,voice,video,cutout,captions,denoise"`
	Status            string             `json:"status" enums:"queued,running,settling,done,failed"`
	Prompt            string             `json:"prompt,omitempty"`
	Aspect            string             `json:"aspect,omitempty" enums:"square,portrait,story"`
	ReferenceMediaIDs []string           `json:"referenceMediaIds"`
	Voice             string             `json:"voice,omitempty"`
	Video             *mediagen.Timeline `json:"video,omitempty"`
	SourceMediaID     string             `json:"sourceMediaId,omitempty"`
	MediaID           string             `json:"mediaId,omitempty"`
	MediaURL          string             `json:"mediaUrl,omitempty"`
	Model             string             `json:"model,omitempty"`
	FailureCode       string             `json:"failureCode,omitempty" enums:"generation_failed,storage_failed,timed_out,enqueue_failed,insufficient_funds,reference_unavailable,cost_unreported"`
	CreatedAt         time.Time          `json:"createdAt"`
	UpdatedAt         time.Time          `json:"updatedAt"`
}

func PresentJob(job *mediagen.Job) JobResponse {
	out := JobResponse{
		ID:                job.ID,
		Kind:              string(job.Kind),
		Status:            string(job.Status),
		Prompt:            job.Prompt,
		Aspect:            string(job.Aspect),
		ReferenceMediaIDs: append([]string{}, job.ReferenceMediaIDs...),
		Voice:             job.Voice,
		SourceMediaID:     job.SourceMediaID,
		FailureCode:       string(job.FailureCode),
		CreatedAt:         job.CreatedAt,
		UpdatedAt:         job.UpdatedAt,
	}
	if job.Kind == mediagen.KindVideo {
		video := job.Video
		out.Video = &video
	}
	if result, ok := job.Delivered(); ok {
		out.MediaID, out.MediaURL, out.Model = result.MediaID, result.MediaURL, result.Model
	}
	return out
}

// @Summary		Modelos de geração de mídia
// @Description	Modelos que geram o tipo pedido (kind: image, music ou voice), do mais usado para o menos usado segundo o provedor; default marca o recomendado. O id escolhido vai no campo model de POST /media/generations. Vídeos são montados pelo Vozko e não têm modelo. Responde 503 (models_unavailable) quando a lista não pode ser carregada; nesse caso nada desse tipo pode ser pedido.
// @Tags			Mídia gerada
// @Produce		json
// @Param			kind	query		string	true	"tipo de mídia"	Enums(image, music, voice)
// @Success		200		{array}		ModelResponse
// @Failure		503		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/media/models [get]
func (h *Handler) Models(w http.ResponseWriter, r *http.Request) {
	if _, ok := httpx.RequireWorkspace(w, r); !ok {
		return
	}
	kind := mediagen.Kind(r.URL.Query().Get("kind"))
	models, err := h.svc.Models(r.Context(), kind)
	if err != nil {
		WriteError(w, err, "Failed to list the models")
		return
	}
	preferred, _ := mediagen.DefaultModel(kind, models)
	out := make([]ModelResponse, 0, len(models))
	for _, model := range models {
		out = append(out, ModelResponse{ID: model.ID, Name: model.Name, Default: model.ID == preferred.ID})
	}
	response.WriteSuccess(w, http.StatusOK, out)
}

// @Summary		Gerar mídia com IA
// @Description	Coloca na fila uma geração. kind image: prompt, aspect (square 1080x1080, portrait 1080x1350, story 1080x1920), model e até 16 referenceMediaIds de imagens. kind music: prompt e model (clipes de cerca de 30 s). kind voice: prompt com o roteiro exato, model e voice opcional. kind video: aspect e video, uma linha do tempo (durationMs até 90000, background #rrggbb, faixas visual de baixo para cima e audio, cada clipe com mediaId da biblioteca, startMs, durationMs, trimInMs, fit cover ou contain, transform normalizado de 0 a 1 com centro, volume de 0 a 2 e fades; clipes de uma faixa não se sobrepõem); sem model. kind cutout (remove o fundo de uma imagem, PNG transparente), captions (legendas WebVTT da fala de um áudio ou vídeo) e denoise (áudio limpo de um áudio ou vídeo): sourceMediaId da biblioteca, sem model; processados no Vozko, no máximo 2 ao mesmo tempo por workspace (429 too_many_jobs). Campos inválidos respondem 422 com o código por campo. Responde 202 com o job; acompanhe por GET /media/generations/{id}. Imagem, música e locução são cobradas do saldo pelo custo informado pelo provedor; o resultado só é entregue depois de cobrado (status settling enquanto o provedor não informa o custo). Um pedido igual do mesmo usuário ainda em andamento devolve o mesmo job sem nova cobrança.
// @Tags			Mídia gerada
// @Accept			json
// @Produce		json
// @Param			body	body		GenerateRequest	true	"o que gerar"
// @Success		202		{object}	JobResponse
// @Failure		400		{object}	response.ErrorResponse
// @Failure		402		{object}	response.ErrorResponse
// @Failure		409		{object}	response.ErrorResponse
// @Failure		429		{object}	response.ErrorResponse
// @Failure		422		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/media/generations [post]
func (h *Handler) Generate(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := httpx.RequireWorkspace(w, r)
	if !ok {
		return
	}
	var body GenerateRequest
	if !httpx.DecodeJSON(w, r, &body) {
		return
	}
	job, err := h.svc.Request(r.Context(), body.request(workspaceID), requesterOf(r))
	if err != nil {
		WriteError(w, err, "Failed to queue the generation")
		return
	}
	response.WriteSuccess(w, http.StatusAccepted, PresentJob(job))
}

// @Summary		Consultar geração de mídia
// @Description	Estado de uma geração do workspace: queued, running, settling (pronta, aguardando o provedor informar o custo), done (com mediaId, mediaUrl e model) ou failed (com failureCode). mediaId e mediaUrl só aparecem em done.
// @Tags			Mídia gerada
// @Produce		json
// @Param			id	path		string	true	"id do job"
// @Success		200	{object}	JobResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/media/generations/{id} [get]
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := httpx.RequireWorkspace(w, r)
	if !ok {
		return
	}
	job, err := h.svc.Get(r.Context(), workspaceID, mux.Vars(r)["id"])
	if err != nil {
		WriteError(w, err, "Failed to load the generation")
		return
	}
	response.WriteSuccess(w, http.StatusOK, PresentJob(job))
}

func requesterOf(r *http.Request) string {
	claims := middleware.GetClaims(r)
	if claims == nil {
		return ""
	}
	return claims.UserID
}
