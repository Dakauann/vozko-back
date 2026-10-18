package campaignlaunchhttp

import (
	"context"
	"errors"
	"net/http"

	"github.com/gorilla/mux"

	"vozko/delivery/http/response"
	"vozko/domain/campaign/launchcheck"
	wc "vozko/domain/whatsapp_campaign"
	wd "vozko/domain/workspace/workspace_department"
	"vozko/infra/http/middleware"
)

type Campaigns interface {
	Owned(workspaceID string, departments *wd.DepartmentFilter, campaignID string) (*wc.Campaign, error)
}

type Check interface {
	Run(ctx context.Context, campaignID string) (launchcheck.Report, error)
}

type Handler struct {
	campaigns Campaigns
	check     Check
}

func NewHandler(campaigns Campaigns, check Check) *Handler {
	return &Handler{campaigns: campaigns, check: check}
}

type Response struct {
	Findings          []launchcheck.Finding `json:"findings"`
	Fingerprint       string                `json:"fingerprint"`
	Blocks            bool                  `json:"blocks"`
	NeedsConfirmation bool                  `json:"needsConfirmation"`
}

func toResponse(r launchcheck.Report) Response {
	findings := r.Findings
	if findings == nil {
		findings = []launchcheck.Finding{}
	}
	return Response{Findings: findings, Fingerprint: r.Fingerprint(), Blocks: r.Blocks(), NeedsConfirmation: r.NeedsConfirmation()}
}

// @Summary		Verificação antes de iniciar ou retomar a campanha
// @Description	Lista o que impede o início (block), os riscos que pedem confirmação (confirm) e as recomendações (advice), seguindo as regras da Meta. Para iniciar com riscos, envie o fingerprint retornado em confirmedFingerprint no início da campanha.
// @Tags			Campanhas WhatsApp
// @Produce		json
// @Param			id	path	string	true	"ID da campanha"
// @Success		200	{object}	campaignlaunchhttp.Response
// @Failure		401	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/whatsapp/campaigns/{id}/launch-check [get]
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	campaign, err := h.campaigns.Owned(middleware.GetWorkspaceID(r), middleware.GetDepartmentFilter(r), mux.Vars(r)["id"])
	if errors.Is(err, wc.ErrCampaignNotFound) {
		response.WriteError(w, http.StatusNotFound, "Campaign not found", nil)
		return
	}
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to read the campaign", nil)
		return
	}
	report, err := h.check.Run(r.Context(), campaign.ID)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to check the campaign", nil)
		return
	}
	response.WriteSuccess(w, http.StatusOK, toResponse(report))
}
