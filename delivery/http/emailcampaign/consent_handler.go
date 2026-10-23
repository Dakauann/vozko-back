package emailcampaignhttp

import (
	"context"
	"net/http"

	"vozko/delivery/http/response"
	"vozko/domain/emailconsent"
	emailconsent_usecase "vozko/usecases/emailconsent"
)

type Consent interface {
	Subscribe(ctx context.Context, workspaceID string, in emailconsent_usecase.SubscribeInput) (emailconsent.Subscription, error)
	ImportConsent(ctx context.Context, workspaceID string, rows []emailconsent_usecase.ImportRow) (emailconsent_usecase.ImportResult, error)
	UnsubscribeAddress(ctx context.Context, workspaceID, address string) (emailconsent.Subscription, error)
	CheckToken(token string) error
	Unsubscribe(ctx context.Context, token string, source emailconsent.UnsubscribeSource) (emailconsent_usecase.UnsubscribeResult, error)
}

type ConsentHandler struct {
	consent Consent
	actor   func(w http.ResponseWriter, r *http.Request) (Actor, bool)
}

func NewConsentHandler(consent Consent) *ConsentHandler {
	return &ConsentHandler{consent: consent, actor: (&Handler{}).actor}
}

// @Summary		Registrar consentimento de e-mail
// @Description	Grava, como evidência opcional, que o endereço aceitou receber e-mail marketing, com a origem, a data e o IP (obrigatório para formulários). O envio não depende deste registro: o consentimento é responsabilidade do cliente e só descadastros, opt-out geral, bloqueios e supressões impedem o envio. Um consentimento importado nunca reativa quem se descadastrou. Exige `email_marketing:update`.
// @Tags			E-mail marketing
// @Accept			json
// @Produce		json
// @Param			body	body		emailcampaignhttp.SubscribeRequest	true	"Consentimento"
// @Success		200		{object}	emailcampaignhttp.SubscriptionResponse
// @Failure		400		{object}	response.ErrorResponse	"email_address_invalid, email_source_invalid, email_consent_ip_required, email_consent_ip_invalid, email_consent_date_in_future"
// @Security		BearerAuth
// @Router			/email/subscriptions [post]
func (h *ConsentHandler) Subscribe(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	var req SubscribeRequest
	if !decode(w, r, &req) {
		return
	}
	if req.Source == string(emailconsent.SourceImport) {
		writeError(w, emailconsent.ErrSourceInvalid)
		return
	}
	sub, err := h.consent.Subscribe(r.Context(), a.WorkspaceID, emailconsent_usecase.SubscribeInput{
		Address: req.Address, LeadID: req.LeadID, Source: emailconsent.Source(req.Source), IP: req.IP, ConsentedAt: req.ConsentedAt,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, subscriptionResponse(sub))
}

// @Summary		Importar consentimentos de e-mail
// @Description	Importa até 10.000 linhas com endereço, data do consentimento e IP opcional. Linhas inválidas voltam com o código do problema; endereços descadastrados continuam descadastrados. Exige `email_marketing:update`.
// @Tags			E-mail marketing
// @Accept			json
// @Produce		json
// @Param			body	body		emailcampaignhttp.ImportRequest	true	"Linhas"
// @Success		200		{object}	emailconsent_usecase.ImportResult
// @Failure		400		{object}	response.ErrorResponse	"email_import_empty, email_import_too_large"
// @Security		BearerAuth
// @Router			/email/subscriptions/import [post]
func (h *ConsentHandler) Import(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	var req ImportRequest
	if !decode(w, r, &req) {
		return
	}
	result, err := h.consent.ImportConsent(r.Context(), a.WorkspaceID, req.rows())
	if err != nil {
		writeError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, result)
}

// @Summary		Descadastrar endereço
// @Description	Registra o descadastro feito por um operador. Exige `email_marketing:update`.
// @Tags			E-mail marketing
// @Accept			json
// @Produce		json
// @Param			body	body		emailcampaignhttp.UnsubscribeAddressRequest	true	"Endereço"
// @Success		200		{object}	emailcampaignhttp.SubscriptionResponse
// @Failure		400		{object}	response.ErrorResponse	"email_address_invalid"
// @Security		BearerAuth
// @Router			/email/subscriptions/unsubscribe [post]
func (h *ConsentHandler) UnsubscribeAddress(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	var req UnsubscribeAddressRequest
	if !decode(w, r, &req) {
		return
	}
	sub, err := h.consent.UnsubscribeAddress(r.Context(), a.WorkspaceID, req.Address)
	if err != nil {
		writeError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, subscriptionResponse(sub))
}
