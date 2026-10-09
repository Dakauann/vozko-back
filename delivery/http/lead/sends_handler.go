package lead

import (
	"context"
	"net/http"
	"strings"

	"vozko/delivery/http/httpx"
	"vozko/delivery/http/response"
	"vozko/domain/campaign"
	"vozko/infra/http/middleware"
	leadsend_usecase "vozko/usecases/leadsend"
)

type Sends interface {
	Review(ctx context.Context, req leadsend_usecase.SendRequest) (*campaign.SendReview, error)
	Start(ctx context.Context, req leadsend_usecase.SendRequest) (*campaign.SendReview, error)
	Cancel(ctx context.Context, req leadsend_usecase.SendRequest) error
}

func (h *LeadHandler) sendRequest(w http.ResponseWriter, r *http.Request) (leadsend_usecase.SendRequest, bool) {
	if h.sends == nil {
		response.WriteErrorWithCode(w, http.StatusServiceUnavailable, "lead_sends_unavailable", "Os envios a partir de uma seleção de leads não estão disponíveis neste servidor", nil)
		return leadsend_usecase.SendRequest{}, false
	}
	a, ok := h.requestActor(w, r)
	if !ok {
		return leadsend_usecase.SendRequest{}, false
	}
	var body LeadSendRequest
	if !httpx.DecodeStrictJSON(w, r, &body) {
		return leadsend_usecase.SendRequest{}, false
	}
	return leadsend_usecase.SendRequest{
		Actor: a, Departments: middleware.GetDepartmentFilter(r), Channel: campaign.Channel(strings.TrimSpace(body.Channel)),
		CampaignIDs: body.CampaignIDs, FirstN: body.FirstN,
	}, true
}

// @Summary		Revisar um envio preparado a partir de leads
// @Description	Relê o disparo parado que POST /leads/actions (`send_template` ou `send_unofficial`) preparou: contagens congeladas por motivo (`skipped`: `no_identity`, `blocked`, `opted_out`, `cooldown`, `already_in_running_campaign`, `missing_variable`, `over_cap`), `missingVariables` (lista por variável do modelo: `slot` é o número de {{N}}, `source` a origem do valor, como `lead.district` ou `lead.custom:<chave>`, e `count` quantos leads ficaram sem ela; um lead sem várias variáveis conta em cada uma, então a soma pode passar de `skipped.missing_variable`, e uma variável pode faltar na lista em envios preparados antes desse detalhe), `cooldownDays` (os dias de proteção contra spam que valiam quando os leads foram pulados, o maior registrado nas entradas; ausente quando nenhuma entrada de `cooldown` registrou os dias, como as preparadas antes desse detalhe ou puladas durante o envio), as contadas mas enviadas (`counted`: `window_open`, a Vozko cobra o modelo mesmo assim, e `no_consent_recorded`, só informativo: um lead sem consentimento registrado nunca é pulado por isso), e a cotação atual (`quote`: custo em micros de US$ com a moeda do saldo, saldo, `capRemaining` do limite mensal, `fits` e `refusal` quando nem todos cabem; no não oficial, `dailyCap` e `estimatedDays`). `campaignIds` são todas as partes do envio (até 10, uma por campanha de até 150.000 leads). `channel`: `official` ou `unofficial`. Exige `leads.send_template` ou `leads.send_unofficial`, e as campanhas precisam ser visíveis pelo departamento de quem pede.
// @Tags			Leads
// @Accept			json
// @Produce		json
// @Param			body	body		LeadSendRequest	true	"Canal e campanhas do envio"
// @Success		200		{object}	campaign.SendReview
// @Failure		400		{object}	response.ErrorResponse
// @Failure		403		{object}	response.ErrorResponse
// @Failure		404		{object}	response.ErrorResponse
// @Failure		503		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/leads/actions/sends/review [post]
func (h *LeadHandler) ReviewSend(w http.ResponseWriter, r *http.Request) {
	req, ok := h.sendRequest(w, r)
	if !ok {
		return
	}
	review, err := h.sends.Review(r.Context(), req)
	if err != nil {
		writeSendError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, review)
}

// @Summary		Enviar um envio preparado a partir de leads
// @Description	Inicia as campanhas paradas do envio ("Enviar"). Antes de iniciar, confere o custo dos que recebem contra o saldo e o limite mensal: se nem todos cabem, responde 409 `unaffordable` ou `over_cap` com `fits`, quantos cabem. Antes de conferir o custo, marca como `already_in_running_campaign` os leads que entraram em outra campanha em andamento depois da revisão. Enviar `firstN` (até `fits`) envia só para N dos destinatários, os de menor id de lead, parte por parte; os demais ficam como `over_cap` e uma parte sem ninguém não é iniciada. Um envio já iniciado responde a revisão atual sem iniciar de novo. Exige `leads.send_template` ou `leads.send_unofficial`.
// @Tags			Leads
// @Accept			json
// @Produce		json
// @Param			body	body		LeadSendRequest	true	"Canal, campanhas e, opcional, os primeiros N"
// @Success		200		{object}	campaign.SendReview
// @Failure		400		{object}	response.ErrorResponse
// @Failure		403		{object}	response.ErrorResponse
// @Failure		404		{object}	response.ErrorResponse
// @Failure		409		{object}	SendBudgetRefusalResponse
// @Failure		503		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/leads/actions/sends/start [post]
func (h *LeadHandler) StartSend(w http.ResponseWriter, r *http.Request) {
	req, ok := h.sendRequest(w, r)
	if !ok {
		return
	}
	review, err := h.sends.Start(r.Context(), req)
	if err != nil {
		writeSendError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, review)
}

// @Summary		Cancelar um envio preparado a partir de leads
// @Description	Exclui as campanhas paradas do envio, quando o diálogo é fechado sem enviar. A exclusão só acontece com a campanha ainda parada, na mesma operação: um envio que já começou, mesmo que ao mesmo tempo, responde 409 `send_already_started` e nada é excluído. Exige `leads.send_template` ou `leads.send_unofficial`.
// @Tags			Leads
// @Accept			json
// @Produce		json
// @Param			body	body		LeadSendRequest	true	"Canal e campanhas do envio"
// @Success		200		{object}	LeadSendCancelResponse
// @Failure		400		{object}	response.ErrorResponse
// @Failure		403		{object}	response.ErrorResponse
// @Failure		404		{object}	response.ErrorResponse
// @Failure		409		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/leads/actions/sends/cancel [post]
func (h *LeadHandler) CancelSend(w http.ResponseWriter, r *http.Request) {
	req, ok := h.sendRequest(w, r)
	if !ok {
		return
	}
	if err := h.sends.Cancel(r.Context(), req); err != nil {
		writeSendError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, LeadSendCancelResponse{Cancelled: true})
}
