package handlers

import (
	"errors"
	"net/http"
	"sort"

	"vozko/delivery/http/response"
	wc "vozko/domain/whatsapp_campaign"
)

var campaignEditRefusals = []response.ErrorRefusal{
	{Err: wc.ErrCampaignScheduledStartInvalid, Refusal: response.Refusal{Status: http.StatusBadRequest, Code: "campaign_schedule_in_past", Message: "O início agendado já passou."}},
	{Err: wc.ErrCampaignScheduledStartTooSoon, Refusal: response.Refusal{Status: http.StatusBadRequest, Code: "campaign_schedule_out_of_window", Message: "O início agendado deve ficar entre 5 minutos e 1 ano a partir de agora."}},
	{Err: wc.ErrCampaignRunning, Refusal: response.Refusal{Status: http.StatusConflict, Code: "campaign_running", Message: "Pause a campanha antes de mudar o agendamento."}},
	{Err: wc.ErrCampaignTemplateNotApproved, Refusal: response.Refusal{Status: http.StatusUnprocessableEntity, Code: "campaign_template_not_ready", Message: "O template escolhido não está pronto para envio."}},
	{Err: wc.ErrCampaignTemplateNotFound, Refusal: response.Refusal{Status: http.StatusUnprocessableEntity, Code: "campaign_template_unavailable", Message: "O template escolhido não está disponível para este workspace."}},
	{Err: wc.ErrCampaignTemplatePhoneMismatch, Refusal: response.Refusal{Status: http.StatusUnprocessableEntity, Code: "campaign_template_phone_mismatch", Message: "O template e o número precisam ser da mesma conta do WhatsApp."}},
	{Err: wc.ErrCampaignTemplateVariablesMismatch, Refusal: response.Refusal{Status: http.StatusBadRequest, Code: "campaign_template_variables_missing", Message: "Há contatos aguardando envio sem as variáveis que o template pede."}},
	{Err: wc.ErrCampaignTemplateVariableEmpty, Refusal: response.Refusal{Status: http.StatusBadRequest, Code: "campaign_template_variables_missing", Message: "Há contatos aguardando envio sem as variáveis que o template pede."}},
	{Err: wc.ErrCampaignBusinessPhoneNotFound, Refusal: response.Refusal{Status: http.StatusUnprocessableEntity, Code: "campaign_phone_unavailable", Message: "O número escolhido não está disponível."}},
	{Err: wc.ErrCampaignBusinessPhoneNoAccess, Refusal: response.Refusal{Status: http.StatusForbidden, Code: "campaign_phone_unavailable", Message: "O número escolhido não está disponível."}},
	{Err: wc.ErrReceptiveManagedByNumber, Refusal: response.Refusal{Status: http.StatusConflict, Code: "campaign_receptive_managed_by_number", Message: "O receptivo é configurado no próprio número."}},
}

var templateNotReadyRefusal = response.Refusal{Status: http.StatusUnprocessableEntity, Code: "campaign_template_not_ready", Message: "O template escolhido não está pronto para envio."}

func writeCampaignEditRefusal(w http.ResponseWriter, err error) bool {
	var notReady *wc.TemplateNotReadyError
	if errors.As(err, &notReady) {
		response.WriteRefusal(w, templateNotReadyRefusal)
		return true
	}
	return response.WriteErrorRefusal(w, err, campaignEditRefusals)
}

func CampaignEditRefusalCodes() []string {
	seen := map[string]bool{}
	var codes []string
	for _, code := range response.RefusalCodes(response.ErrorRefusalsOf(campaignEditRefusals)) {
		if !seen[code] {
			seen[code] = true
			codes = append(codes, code)
		}
	}
	sort.Strings(codes)
	return codes
}
