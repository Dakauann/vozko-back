package smartcampaignhttp

import (
	"encoding/json"
	"net/http"
	"time"

	"vozko/delivery/http/response"
	sc "vozko/domain/whatsapp_campaign/smartcampaign"
)

type ProjectionRequest struct {
	Contacts int        `json:"contacts" example:"10000"`
	StartAt  *time.Time `json:"startAt,omitempty"`
	Policy   PolicyBody `json:"policy"`
}

type ProjectedWave struct {
	Number int       `json:"number" example:"1"`
	Size   int       `json:"size" example:"200"`
	At     time.Time `json:"at"`
}

type ProjectionResponse struct {
	Waves     []ProjectedWave `json:"waves"`
	Truncated bool            `json:"truncated"`
}

// @Summary		Previsão das ondas do disparo inteligente
// @Description	Calcula quando cada onda pode sair, no melhor caso: sem alertas da Meta, só com a espera de observação, o horário de envio e o limite diário. Alertas reduzem, pausam ou adiam as ondas. Usa a regra de tamanho do próprio motor. Sem startAt, a previsão começa agora.
// @Tags			Campanhas WhatsApp
// @Accept			json
// @Produce		json
// @Param			body	body		smartcampaignhttp.ProjectionRequest	true	"Contatos, início e política"
// @Success		200		{object}	smartcampaignhttp.ProjectionResponse
// @Failure		400		{object}	response.ErrorResponse
// @Failure		401		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/whatsapp/campaign-waves/projection [post]
func (h *Handler) Projection(w http.ResponseWriter, r *http.Request) {
	var body ProjectionRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Contacts <= 0 {
		response.WriteError(w, http.StatusBadRequest, "Send the number of contacts and the wave policy", nil)
		return
	}
	policy, err := body.Policy.ToDomain()
	if err == nil {
		err = policy.Validate()
	}
	start := time.Now()
	if body.StartAt != nil && body.StartAt.After(start) {
		start = *body.StartAt
	}
	var plan sc.Projection
	if err == nil {
		plan, err = sc.Project(policy, body.Contacts, start)
	}
	if err != nil {
		if !WriteRefusal(w, err) {
			response.WriteError(w, http.StatusBadRequest, "Invalid wave policy", nil)
		}
		return
	}
	waves := make([]ProjectedWave, len(plan.Waves))
	for i, wave := range plan.Waves {
		waves[i] = ProjectedWave{Number: wave.Number, Size: wave.Size, At: wave.At}
	}
	response.WriteSuccess(w, http.StatusOK, ProjectionResponse{Waves: waves, Truncated: plan.Truncated})
}
