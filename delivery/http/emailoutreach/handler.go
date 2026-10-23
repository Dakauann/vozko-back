package emailoutreach

import (
	"context"
	"net/http"

	"vozko/delivery/http/emailpricing"
	"vozko/delivery/http/httpx"
	"vozko/delivery/http/response"
	"vozko/usecases/emailoutreach"
)

type StartUseCase interface {
	Execute(ctx context.Context, in emailoutreach.Input) (emailoutreach.Started, error)
}

type Handler struct {
	start       StartUseCase
	prices      emailpricing.PriceList
	departments httpx.DepartmentScopeResolver
}

type HandlerDeps struct {
	Start       StartUseCase
	Prices      emailpricing.PriceList
	Departments httpx.DepartmentScopeResolver
}

func NewHandler(d HandlerDeps) *Handler {
	return &Handler{start: d.Start, prices: d.Prices, departments: d.Departments}
}

// @Summary		Iniciar conversa por e-mail
// @Description	Envia um e-mail individual a um lead (`lead_id`) ou a um endereço (`address`, o lead é encontrado pelo e-mail ou um contato de e-mail é criado) e abre ou reaproveita a conversa de e-mail desse contato no inbox, com o e-mail registrado nela pelo operador e a conversa atribuída a ele quando ainda não tem responsável. O corpo é `text` (texto simples com {{first_name}}, {{name}}, {{email}}, {{company}} e {{field.chave}}) ou `design_id` (um modelo salvo), nunca os dois; `sender_id` vazio usa o remetente padrão. Cada envio é cobrado da carteira pelo preço de `GET /email/outreach/quote` e devolvido se o provedor recusar. `idempotency_key` é obrigatória e deve ser gerada uma vez por composição: repetir a mesma chave devolve a mesma conversa e a mesma mensagem (200) sem cobrar nem enviar de novo. Recusas antes de qualquer cobrança: 400 `email_idempotency_key_required`, `email_recipient_missing`, `email_address_invalid`; 403 `email_lead_blocked`, `email_outreach_forbidden` (remetente de um departamento fora do seu acesso); 404 `email_lead_not_found`, `email_design_not_found`; 409 `email_lead_opted_out`, `email_sender_missing`, `email_send_in_progress`; 422 `email_lead_no_email`, `email_message_invalid`, `email_subject_invalid`; 402 `email_balance_insufficient` (details.missing_micros). Depois da cobrança, com reembolso: 409 `email_recipient_suppressed`, `email_sender_not_verified`, `email_account_paused`; 429 `email_daily_cap_reached`; 429 `email_send_throttled` quando o provedor está no limite de envio agora (cabeçalho `Retry-After` e `details.retry_after_seconds` em segundos inteiros; repita com a mesma `idempotency_key` depois desse tempo, o envio sai uma vez e é cobrado uma vez); 503 `email_account_reactivation_failed`; 502 `email_send_failed`. `recorded` falso quer dizer que o e-mail saiu mas não pôde ser registrado no inbox; repetir a mesma chave tenta registrar de novo sem reenviar.
// @Tags			E-mail
// @Accept			json
// @Produce		json
// @Param			request	body		StartConversationRequest		true	"Destinatário, remetente e conteúdo"
// @Success		201		{object}	StartedConversationResponse
// @Success		200		{object}	StartedConversationResponse	"Envio com a mesma chave já realizado; nada foi cobrado novamente"
// @Failure		400		{object}	response.ErrorResponse
// @Failure		402		{object}	response.ErrorResponse	"email_balance_insufficient (details.missing_micros)"
// @Failure		403		{object}	response.ErrorResponse
// @Failure		404		{object}	response.ErrorResponse
// @Failure		409		{object}	response.ErrorResponse
// @Failure		422		{object}	response.ErrorResponse
// @Failure		429		{object}	response.ErrorResponse	"email_daily_cap_reached ou email_send_throttled (details.retry_after_seconds)"
// @Header		429		{integer}	Retry-After	"Segundos até tentar de novo, quando email_send_throttled"
// @Failure		502		{object}	response.ErrorResponse
// @Failure		503		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/email/outreach/conversations [post]
func (h *Handler) StartConversation(w http.ResponseWriter, r *http.Request) {
	scope, ok := httpx.RequireSenderScope(w, r, h.departments)
	if !ok {
		return
	}
	var req StartConversationRequest
	if !httpx.DecodeStrictJSON(w, r, &req) {
		return
	}
	started, err := h.start.Execute(r.Context(), emailoutreach.Input{
		WorkspaceID:    scope.WorkspaceID,
		UserID:         scope.UserID,
		IsAdmin:        scope.IsAdmin,
		Departments:    scope.Departments,
		LeadID:         req.LeadID,
		Address:        req.Address,
		SenderID:       req.SenderID,
		Subject:        req.Subject,
		Text:           req.Text,
		DesignID:       req.DesignID,
		IdempotencyKey: req.IdempotencyKey,
	})
	if err != nil {
		writeStartRefusal(w, scope.WorkspaceID, err)
		return
	}
	status := http.StatusCreated
	if started.Repeated {
		status = http.StatusOK
	}
	response.WriteSuccess(w, status, StartedConversationResponse{
		ConversationID: started.ConversationID,
		EntryType:      started.EntryType,
		MessageID:      started.MessageID,
		Repeated:       started.Repeated,
		Recorded:       started.Recorded,
	})
}

// @Summary		Consultar o preço de um e-mail individual
// @Description	Retorna o preço de um e-mail enviado para o workspace, já com o plano aplicado, em micros de dólar e em reais pela cotação atual. É o valor cobrado por `POST /email/outreach/conversations`. Se o preço ou a cotação não puderem ser lidos, a resposta é uma recusa e nenhum preço é inventado: 503 `email_exchange_rate_unavailable` ou `email_pricing_unavailable`, 500 `email_pricing_failed`.
// @Tags			E-mail
// @Produce		json
// @Success		200	{object}	QuoteResponse
// @Failure		403	{object}	response.ErrorResponse	"Workspace obrigatório"
// @Failure		500	{object}	response.ErrorResponse	"email_pricing_failed"
// @Failure		503	{object}	response.ErrorResponse	"email_exchange_rate_unavailable ou email_pricing_unavailable"
// @Security		BearerAuth
// @Router			/email/outreach/quote [get]
func (h *Handler) Quote(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := httpx.RequireWorkspace(w, r)
	if !ok {
		return
	}
	prices, err := h.prices.Execute(r.Context(), workspaceID)
	if err != nil {
		emailpricing.WritePriceListRefusal(w, workspaceID, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, QuoteResponse{
		UnitMicros:            prices.Send.UnitMicros,
		UnitBRL:               prices.Send.UnitBRL,
		ExchangeRate:          prices.ExchangeRate,
		ExchangeRateUpdatedAt: prices.ExchangeRateUpdatedAt,
	})
}
