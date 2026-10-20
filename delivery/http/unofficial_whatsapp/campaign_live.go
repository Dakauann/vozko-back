package unofficial_whatsapp

import (
	"net/http"

	"vozko/delivery/http/campaignlivehttp"
	"vozko/domain/campaign"
	uwc "vozko/domain/unofficial_whatsapp_campaign"
)

func (h *CampaignHandler) SetLive(progress campaign.ProgressUseCase, timeline campaign.TimelineUseCase) {
	h.progress = progress
	h.timeline = timeline
}

func liveOfUnofficial(c *uwc.Campaign) campaign.LiveCampaign {
	return campaign.LiveCampaign{ID: c.ID, WorkspaceID: c.WorkspaceID, DepartmentID: c.DepartmentID, Status: c.Status}
}

// @Summary		Progresso ao vivo de uma campanha não oficial
// @Description	Mesmas regras do progresso da campanha oficial: contagens por status, métricas e ritmo de envio em cache compartilhado por campanha (4 segundos em execução, 30 segundos nos demais estados), status sempre lido agora e `refreshAfterSeconds` dizendo quando pedir de novo.
// @Tags			Unofficial WhatsApp Campaigns
// @Produce		json
// @Param			id	path		string	true	"Campanha"
// @Success		200	{object}	campaign.Progress
// @Failure		403	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Failure		503	{object}	response.ErrorResponse	"campaign_live_unavailable"
// @Security		BearerAuth
// @Router			/unofficial-whatsapp/campaigns/{id}/progress [get]
func (h *CampaignHandler) Progress(w http.ResponseWriter, r *http.Request) {
	_, _, c, ok := h.ownedCampaign(w, r)
	if !ok {
		return
	}
	campaignlivehttp.WriteProgress(w, r, h.progress, liveOfUnofficial(c))
}

// @Summary		Linha do tempo de uma campanha não oficial
// @Description	Mesmas regras da linha do tempo da campanha oficial: envios, entregas, leituras e falhas por minuto, hora ou dia, no fuso do workspace, com a escala escolhida pelo servidor quando `bucket` vem vazio, cache compartilhado e o limite de consultas analíticas.
// @Tags			Unofficial WhatsApp Campaigns
// @Produce		json
// @Param			id		path		string	true	"Campanha"
// @Param			bucket	query		string	false	"minute, hour ou day"
// @Success		200		{object}	campaign.Timeline
// @Failure		400		{object}	response.ErrorResponse	"campaign_timeline_bucket_invalid"
// @Failure		403		{object}	response.ErrorResponse
// @Failure		404		{object}	response.ErrorResponse
// @Failure		503		{object}	response.ErrorResponse	"campaign_live_unavailable"
// @Failure		504		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/unofficial-whatsapp/campaigns/{id}/timeline [get]
func (h *CampaignHandler) Timeline(w http.ResponseWriter, r *http.Request) {
	_, _, c, ok := h.ownedCampaign(w, r)
	if !ok {
		return
	}
	campaignlivehttp.WriteTimeline(w, r, h.timeline, liveOfUnofficial(c))
}
