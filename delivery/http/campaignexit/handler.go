package campaignexithttp

import (
	"context"
	"errors"
	"log"
	"net/http"

	"vozko/delivery/http/httpx"
	"vozko/delivery/http/response"
	"vozko/domain/campaign"
	campaignexit_usecase "vozko/usecases/campaignexit"
)

type AudienceExits interface {
	AudienceExits(ctx context.Context, workspaceID string, numbers []string) (campaign.AudienceExits, error)
}

type Handler struct {
	exits AudienceExits
}

func NewHandler(exits AudienceExits) *Handler {
	return &Handler{exits: exits}
}

type AudienceExitsRequest struct {
	Numbers []string `json:"numbers" example:"5511987650001"`
}

type AudienceExitsResponse struct {
	OptedOut     int `json:"optedOut" example:"30"`
	Blocked      int `json:"blocked" example:"7"`
	NotReceiving int `json:"notReceiving" example:"37"`
}

var refusalStatuses = []struct {
	err    error
	status int
}{
	{campaignexit_usecase.ErrWorkspaceRequired, http.StatusForbidden},
	{campaignexit_usecase.ErrTooManyNumbers, http.StatusRequestEntityTooLarge},
	{campaignexit_usecase.ErrUnavailable, http.StatusServiceUnavailable},
}

func writeError(w http.ResponseWriter, err error) {
	if errors.Is(err, context.Canceled) {
		return
	}
	if httpx.WriteAnalyticsLimit(w, err, "Campaign audience") {
		return
	}
	for _, known := range refusalStatuses {
		if errors.Is(err, known.err) {
			response.WriteErrorWithCode(w, known.status, campaignexit_usecase.ErrorCode(err), err.Error(), nil)
			return
		}
	}
	log.Printf("[campaign-exit] %v", err)
	response.WriteError(w, http.StatusInternalServerError, "Failed to count who left", nil)
}

// @Summary		Quem da lista não recebe
// @Description	Para a lista de números do formulário de campanha, quantos já não recebem mensagens: os que pediram para sair (`optedOut`) e os bloqueados (`blocked`), pela mesma regra que a campanha usa ao criar as entradas; um lead bloqueado que também saiu conta como bloqueado. Os números são lidos como na criação da campanha (só dígitos, com o código do país; o nono dígito é conferido nas duas formas). Nada é gravado. Conta só leads que já existem no workspace. Até 150000 números (413 `campaign_exit_too_many_numbers`); a contagem fica em cache por pouco tempo e muda quando um lead muda.
// @Tags			Campanhas de WhatsApp
// @Accept			json
// @Produce		json
// @Param			body	body		AudienceExitsRequest	true	"números da lista"
// @Success		200		{object}	AudienceExitsResponse
// @Failure		400		{object}	response.ErrorResponse
// @Failure		413		{object}	response.ErrorResponse
// @Failure		503		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/whatsapp/campaigns/exit-audience [post]
func (h *Handler) AudienceExits(w http.ResponseWriter, r *http.Request) {
	if h.exits == nil {
		writeError(w, campaignexit_usecase.ErrUnavailable)
		return
	}
	viewer, ok := httpx.WorkspaceActor(r)
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, "authentication required", nil)
		return
	}
	var body AudienceExitsRequest
	if !httpx.DecodeStrictJSON(w, r, &body) {
		return
	}
	exits, err := h.exits.AudienceExits(r.Context(), viewer.WorkspaceID, body.Numbers)
	if err != nil {
		writeError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, AudienceExitsResponse{OptedOut: exits.OptedOut, Blocked: exits.Blocked, NotReceiving: exits.NotReceiving()})
}
