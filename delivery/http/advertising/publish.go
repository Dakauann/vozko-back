package advertisinghttp

import (
	"errors"
	"net/http"
	"time"

	"github.com/gorilla/mux"

	"vozko/delivery/http/response"
	"vozko/domain/advertising"
	adsuc "vozko/usecases/advertising"
)

type DraftRequest struct {
	Draft advertising.AdDraft `json:"draft"`
}

type FeeResponse struct {
	Price    int64  `json:"price"`
	Total    int64  `json:"total"`
	Currency string `json:"currency"`
}

type ValidateResponse struct {
	Issues []advertising.FieldIssue `json:"issues"`
	Fee    *FeeResponse             `json:"fee,omitempty"`
}

type JobResponse struct {
	ID           string               `json:"id"`
	AdAccountID  string               `json:"adAccountId"`
	CampaignName string               `json:"campaignName"`
	Status       string               `json:"status"`
	Progress     advertising.Progress `json:"progress"`
	Fee          string               `json:"fee"`
	ErrorCode    string               `json:"errorCode,omitempty"`
	ErrorMessage string               `json:"errorMessage,omitempty"`
	CreatedAt    time.Time            `json:"createdAt"`
	UpdatedAt    time.Time            `json:"updatedAt"`
}

type GenerateImageRequest struct {
	Prompt string `json:"prompt"`
	Aspect string `json:"aspect"`
}

type GeneratedImageResponse struct {
	MediaID string `json:"mediaId"`
	URL     string `json:"url"`
	Model   string `json:"model"`
}

type ConversationOriginResponse struct {
	AdID              string `json:"adId"`
	AdName            string `json:"adName"`
	AdSetName         string `json:"adSetName,omitempty"`
	CampaignName      string `json:"campaignName,omitempty"`
	AccountName       string `json:"accountName"`
	Currency          string `json:"currency"`
	Day               string `json:"day"`
	DaySpend          int64  `json:"daySpend"`
	DayConversations  int64  `json:"dayConversations"`
	EstimatedLeadCost *int64 `json:"estimatedLeadCost"`
}

// @Summary		Validar rascunho de anúncio
// @Description	Valida o rascunho, a conta, a página, o número de WhatsApp e as mídias, e informa a taxa por anúncio publicado (fee.price) e o total do rascunho (fee.total). Problemas voltam em issues.
// @Tags			Anúncios
// @Accept			json
// @Produce		json
// @Param			body	body		DraftRequest	true	"rascunho"
// @Success		200		{object}	ValidateResponse
// @Failure		409		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/ads/drafts/validate [post]
func (h *Handler) ValidateDraft(w http.ResponseWriter, r *http.Request) {
	var req DraftRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	pre, err := h.d.Publish.Preflight(r.Context(), workspaceOf(r), req.Draft)
	var invalid *advertising.ValidationError
	if errors.As(err, &invalid) {
		response.WriteSuccess(w, http.StatusOK, ValidateResponse{Issues: invalid.Issues})
		return
	}
	if err != nil {
		writeError(w, err, "Failed to validate the ad")
		return
	}
	response.WriteSuccess(w, http.StatusOK, ValidateResponse{Issues: []advertising.FieldIssue{}, Fee: presentFee(pre)})
}

// @Summary		Publicar anúncio
// @Description	Cobra a taxa por anúncio, cria campanha, conjunto, criativos e anúncios pausados na Meta e só então liga tudo. Uma resposta sem confirmação da Meta fica em NEEDS_REVIEW e nunca é recriada sozinha.
// @Tags			Anúncios
// @Accept			json
// @Produce		json
// @Param			body	body		DraftRequest	true	"rascunho"
// @Success		201		{object}	JobResponse
// @Failure		402		{object}	response.ErrorResponse
// @Failure		422		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/ads/publish [post]
func (h *Handler) Publish(w http.ResponseWriter, r *http.Request) {
	var req DraftRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	job, err := h.d.Publish.Publish(r.Context(), adsuc.PublishInput{
		WorkspaceID: workspaceOf(r), UserID: personOf(r).UserID, Actor: advertising.ActorPerson, Draft: req.Draft,
	})
	if err != nil {
		writeError(w, err, "Failed to publish the ad")
		return
	}
	response.WriteSuccess(w, http.StatusCreated, presentJob(job))
}

// @Summary		Listar publicações de anúncios
// @Tags			Anúncios
// @Produce		json
// @Success		200	{array}	JobResponse
// @Security		BearerAuth
// @Router			/ads/publish-jobs [get]
func (h *Handler) ListJobs(w http.ResponseWriter, r *http.Request) {
	jobs, err := h.d.Publish.Jobs(r.Context(), workspaceOf(r), maxJobsListed)
	if err != nil {
		writeError(w, err, "Failed to list publish jobs")
		return
	}
	response.WriteSuccess(w, http.StatusOK, presentAll(jobs, presentJob))
}

// @Summary		Detalhe de publicação de anúncio
// @Tags			Anúncios
// @Produce		json
// @Param			id	path		string	true	"ID da publicação"
// @Success		200	{object}	JobResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/ads/publish-jobs/{id} [get]
func (h *Handler) GetJob(w http.ResponseWriter, r *http.Request) {
	job, err := h.d.Publish.Job(r.Context(), workspaceOf(r), mux.Vars(r)["id"])
	if err != nil {
		writeError(w, err, "Failed to load the publish job")
		return
	}
	response.WriteSuccess(w, http.StatusOK, presentJob(job))
}

// @Summary		Gerar imagem para anúncio com IA
// @Description	Gera a imagem no formato pedido (square 1080x1080, portrait 1080x1350, story 1080x1920), salva na biblioteca de mídia e cobra como uso de IA.
// @Tags			Anúncios
// @Accept			json
// @Produce		json
// @Param			body	body		GenerateImageRequest	true	"descrição da imagem"
// @Success		201		{object}	GeneratedImageResponse
// @Failure		402		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/ads/images [post]
func (h *Handler) GenerateImage(w http.ResponseWriter, r *http.Request) {
	var req GenerateImageRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	image, err := h.d.Images.Generate(r.Context(), advertising.ImageRequest{
		WorkspaceID: workspaceOf(r), Prompt: req.Prompt, Aspect: advertising.Aspect(req.Aspect),
	})
	if err != nil {
		writeError(w, err, "Failed to generate the image")
		return
	}
	response.WriteSuccess(w, http.StatusCreated, GeneratedImageResponse{MediaID: image.Media.ID, URL: image.Media.URL, Model: image.Model})
}

// @Summary		Anúncio de origem de uma conversa
// @Description	Campanha, conjunto e anúncio de onde a conversa veio, com o custo estimado do lead (gasto do anúncio no dia dividido pelas conversas do dia). 404 quando o anúncio não é de uma conta conectada.
// @Tags			Anúncios
// @Produce		json
// @Param			entryType	path		string	true	"canal da conversa"
// @Param			entryId		path		string	true	"ID da conversa"
// @Success		200			{object}	ConversationOriginResponse
// @Failure		403			{object}	response.ErrorResponse
// @Failure		404			{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/ads/conversations/{entryType}/{entryId}/origin [get]
func (h *Handler) ConversationOrigin(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	origin, err := h.d.Origins.Origin(r.Context(), personOf(r), workspaceOf(r), vars["entryType"], vars["entryId"])
	if err != nil {
		writeError(w, err, "Failed to load the ad origin")
		return
	}
	response.WriteSuccess(w, http.StatusOK, presentOrigin(origin))
}

func presentFee(pre *adsuc.Preflight) *FeeResponse {
	return &FeeResponse{Price: pre.Fee.PriceMicros, Total: pre.FeeTotal.PriceMicros, Currency: pre.Fee.Currency}
}

func presentJob(j *advertising.PublishJob) JobResponse {
	return JobResponse{
		ID: j.ID, AdAccountID: j.AdAccountID, CampaignName: j.Draft.Campaign.Name, Status: string(j.Status),
		Progress: j.Progress, Fee: string(j.Fee), ErrorCode: j.ErrorCode, ErrorMessage: j.ErrorMessage,
		CreatedAt: j.CreatedAt, UpdatedAt: j.UpdatedAt,
	}
}

func presentOrigin(o *adsuc.ConversationOrigin) ConversationOriginResponse {
	out := ConversationOriginResponse{
		AdID: o.Ad.MetaID, AdName: o.Ad.Name, AccountName: o.Account.Name, Currency: o.Account.Currency,
		Day: o.Cost.Day.Format(advertising.DayLayout), DaySpend: o.Cost.SpendMicros, DayConversations: o.Cost.Conversations,
		EstimatedLeadCost: o.Cost.Estimate(),
	}
	if o.AdSet != nil {
		out.AdSetName = o.AdSet.Name
	}
	if o.Campaign != nil {
		out.CampaignName = o.Campaign.Name
	}
	return out
}
