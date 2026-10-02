package imagegenhttp

import (
	"context"
	"net/http"
	"time"

	"github.com/gorilla/mux"

	"vozko/delivery/http/httpx"
	"vozko/delivery/http/response"
	"vozko/domain/imagegen"
	"vozko/infra/http/middleware"
)

type Service interface {
	Request(ctx context.Context, req imagegen.Request, requestedBy string) (*imagegen.Job, error)
	Get(ctx context.Context, workspaceID, id string) (*imagegen.Job, error)
}

type Handler struct {
	svc Service
}

func NewHandler(svc Service) *Handler {
	return &Handler{svc: svc}
}

type GenerateImageRequest struct {
	Prompt            string   `json:"prompt" example:"Pizza artesanal sobre mesa de madeira, luz natural"`
	Aspect            string   `json:"aspect" enums:"square,portrait,story" example:"square"`
	ReferenceMediaIDs []string `json:"referenceMediaIds,omitempty" maxItems:"16"`
}

type JobResponse struct {
	ID                string    `json:"id"`
	Status            string    `json:"status" enums:"queued,running,done,failed"`
	Prompt            string    `json:"prompt"`
	Aspect            string    `json:"aspect" enums:"square,portrait,story"`
	ReferenceMediaIDs []string  `json:"referenceMediaIds"`
	MediaID           string    `json:"mediaId,omitempty"`
	MediaURL          string    `json:"mediaUrl,omitempty"`
	Model             string    `json:"model,omitempty"`
	FailureCode       string    `json:"failureCode,omitempty" enums:"generation_failed,storage_failed,timed_out,enqueue_failed,insufficient_funds,reference_unavailable"`
	CreatedAt         time.Time `json:"createdAt"`
	UpdatedAt         time.Time `json:"updatedAt"`
}

func presentJob(job *imagegen.Job) JobResponse {
	return JobResponse{
		ID:                job.ID,
		Status:            string(job.Status),
		Prompt:            job.Prompt,
		Aspect:            string(job.Aspect),
		ReferenceMediaIDs: append([]string{}, job.ReferenceMediaIDs...),
		MediaID:           job.MediaID,
		MediaURL:          job.MediaURL,
		Model:             job.Model,
		FailureCode:       string(job.FailureCode),
		CreatedAt:         job.CreatedAt,
		UpdatedAt:         job.UpdatedAt,
	}
}

// @Summary		Gerar imagem com IA
// @Description	Coloca na fila a geração de uma imagem no formato pedido (square 1080x1080, portrait 1080x1350, story 1080x1920). Opcionalmente recebe até 16 imagens de referência da biblioteca de mídia do workspace (referenceMediaIds, sem repetição, só imagens) para edição ou estilo; uma referência inválida responde 422 com o código no campo referenceMediaIds (required, too_many, duplicate, not_found ou not_image). Responde 202 com o job; acompanhe por GET /images/generations/{id} até status done (mediaId e mediaUrl da biblioteca de mídia) ou failed (failureCode). A imagem é cobrada do saldo como uso de IA. Um pedido igual do mesmo usuário nos últimos 10 minutos, ainda em andamento, devolve o mesmo job sem nova cobrança.
// @Tags			Imagens
// @Accept			json
// @Produce		json
// @Param			body	body		GenerateImageRequest	true	"descrição e formato da imagem"
// @Success		202		{object}	JobResponse
// @Failure		400		{object}	response.ErrorResponse
// @Failure		402		{object}	response.ErrorResponse
// @Failure		409		{object}	response.ErrorResponse
// @Failure		422		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/images/generations [post]
func (h *Handler) Generate(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := httpx.RequireWorkspace(w, r)
	if !ok {
		return
	}
	var body GenerateImageRequest
	if !httpx.DecodeJSON(w, r, &body) {
		return
	}
	job, err := h.svc.Request(r.Context(), imagegen.Request{
		WorkspaceID: workspaceID, Prompt: body.Prompt, Aspect: imagegen.Aspect(body.Aspect), ReferenceMediaIDs: body.ReferenceMediaIDs,
	}, requesterOf(r))
	if err != nil {
		writeError(w, err, "Failed to queue the image generation")
		return
	}
	response.WriteSuccess(w, http.StatusAccepted, presentJob(job))
}

// @Summary		Consultar geração de imagem
// @Description	Estado de uma geração de imagem do workspace: queued, running, done (com mediaId, mediaUrl e model) ou failed (com failureCode: generation_failed, storage_failed, timed_out, enqueue_failed, insufficient_funds ou reference_unavailable).
// @Tags			Imagens
// @Produce		json
// @Param			id	path		string	true	"id do job"
// @Success		200	{object}	JobResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/images/generations/{id} [get]
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := httpx.RequireWorkspace(w, r)
	if !ok {
		return
	}
	job, err := h.svc.Get(r.Context(), workspaceID, mux.Vars(r)["id"])
	if err != nil {
		writeError(w, err, "Failed to load the image generation")
		return
	}
	response.WriteSuccess(w, http.StatusOK, presentJob(job))
}

func requesterOf(r *http.Request) string {
	claims := middleware.GetClaims(r)
	if claims == nil {
		return ""
	}
	return claims.UserID
}
