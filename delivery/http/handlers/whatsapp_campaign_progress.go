package handlers

import (
	"net/http"

	"github.com/gorilla/mux"

	"vozko/delivery/http/campaignlivehttp"
	"vozko/domain/campaign"
	wc "vozko/domain/whatsapp_campaign"
)

func (h *WhatsAppCampaignHandler) SetLive(progress campaign.ProgressUseCase, timeline campaign.TimelineUseCase) {
	h.progress = progress
	h.timeline = timeline
}

func liveOfOfficial(c *wc.Campaign) campaign.LiveCampaign {
	return campaign.LiveCampaign{ID: c.ID, WorkspaceID: c.WorkspaceID, DepartmentID: c.DepartmentID, Status: c.Status}
}

// @Summary		Progresso ao vivo de uma campanha oficial
// @Description	Contagens por status, métricas e ritmo de envio (contatos processados por minuto e tempo restante estimado). As contagens ficam em cache compartilhado por campanha: 4 segundos enquanto ela está em execução e 30 segundos nos demais estados, então qualquer número de pessoas olhando custa uma contagem por janela. O status vem sempre da campanha lida agora. `refreshAfterSeconds` diz quando pedir de novo; 0 quer dizer que não há o que acompanhar. O ritmo só aparece em execução e depois de pelo menos 20 segundos de amostra.
// @Tags			WhatsApp Campaigns
// @Produce		json
// @Param			id	path		string	true	"Campanha"
// @Success		200	{object}	campaign.Progress
// @Failure		403	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Failure		503	{object}	response.ErrorResponse	"campaign_live_unavailable"
// @Security		BearerAuth
// @Router			/whatsapp/campaigns/{id}/progress [get]
func (h *WhatsAppCampaignHandler) Progress(w http.ResponseWriter, r *http.Request) {
	owned, ok := h.ownedCampaign(w, mux.Vars(r)["id"], r)
	if !ok {
		return
	}
	campaignlivehttp.WriteProgress(w, r, h.progress, liveOfOfficial(owned))
}

// @Summary		Linha do tempo de uma campanha oficial
// @Description	Envios, entregas, leituras e falhas contados por minuto, hora ou dia a partir dos carimbos de cada contato, no fuso do horário de funcionamento do workspace (ou do departamento). Sem `bucket`, o servidor escolhe o menor que cobre o período de envio: minuto até 3 horas, hora até 7 dias, dia depois disso. Cada escala mostra a janela mais recente que cabe nela (180 minutos, 168 horas ou 92 dias); em execução a janela vai até agora. Contatos enviados antes dos carimbos existirem não aparecem. O resultado fica em cache compartilhado (10 segundos em execução, 5 minutos nos demais estados) e passa pelo limite de consultas analíticas, que responde 503 com Retry-After quando está cheio.
// @Tags			WhatsApp Campaigns
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
// @Router			/whatsapp/campaigns/{id}/timeline [get]
func (h *WhatsAppCampaignHandler) Timeline(w http.ResponseWriter, r *http.Request) {
	owned, ok := h.ownedCampaign(w, mux.Vars(r)["id"], r)
	if !ok {
		return
	}
	campaignlivehttp.WriteTimeline(w, r, h.timeline, liveOfOfficial(owned))
}
