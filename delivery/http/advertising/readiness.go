package advertisinghttp

import (
	"net/http"

	"github.com/gorilla/mux"

	"vozko/delivery/http/response"
	"vozko/domain/advertising"
	adsuc "vozko/usecases/advertising"
)

type ReadinessActionResponse struct {
	Kind string `json:"kind" enums:"in_app,portal"`
	Key  string `json:"key,omitempty" enums:"reconnect,sync,create_pixel"`
	URL  string `json:"url,omitempty"`
}

type ReadinessItemResponse struct {
	Key      string                   `json:"key" enums:"connection,advertiser_role,account_status,account_details,payment_method,page,phone_verification,email_verification,custom_audience_terms,pixel"`
	State    string                   `json:"state" enums:"ready,missing,unknown"`
	Required bool                     `json:"required"`
	Action   *ReadinessActionResponse `json:"action,omitempty"`
}

type BillingResponse struct {
	PortalURL     string `json:"portalUrl"`
	PaymentMethod string `json:"paymentMethod,omitempty"`
	Balance       int64  `json:"balance"`
	Prepay        bool   `json:"prepay"`
}

type ReadinessResponse struct {
	Account  AccountResponse         `json:"account"`
	Ready    bool                    `json:"ready"`
	Blocking []string                `json:"blocking"`
	Items    []ReadinessItemResponse `json:"items"`
	Billing  *BillingResponse        `json:"billing,omitempty"`
}

func presentReadiness(r *adsuc.Readiness) ReadinessResponse {
	out := ReadinessResponse{
		Account:  presentAccount(r.Account),
		Ready:    r.Checklist.CanPublish(),
		Blocking: []string{},
		Items:    make([]ReadinessItemResponse, 0, len(r.Checklist.Items)),
	}
	for _, key := range r.Checklist.Blocking() {
		out.Blocking = append(out.Blocking, string(key))
	}
	for _, item := range r.Checklist.Items {
		out.Items = append(out.Items, ReadinessItemResponse{Key: string(item.Key), State: string(item.State), Required: item.Required, Action: presentReadinessAction(item.Action)})
	}
	if r.Billing != nil {
		out.Billing = &BillingResponse{PortalURL: advertising.BillingPortalURL, PaymentMethod: r.Billing.PaymentMethod, Balance: r.Billing.Balance, Prepay: r.Billing.Prepay}
	}
	return out
}

func presentReadinessAction(a advertising.ReadinessAction) *ReadinessActionResponse {
	switch {
	case a.InApp != "":
		return &ReadinessActionResponse{Kind: "in_app", Key: string(a.InApp)}
	case a.Portal != "":
		return &ReadinessActionResponse{Kind: "portal", URL: a.Portal}
	}
	return nil
}

// @Summary		Prontidão da conta para veicular anúncios
// @Description	Lista ordenada do que a conta precisa para publicar, como na Meta: conexão, função de anunciante, status, moeda e fuso, forma de pagamento, página, verificações de telefone e email, termos de públicos personalizados e pixel. Cada item vem ready, missing ou unknown; unknown nunca conta como pronto. Itens com required true bloqueiam a publicação e aparecem em blocking. A ação de um item é in_app (reconnect, sync, create_pixel, feitas no Vozko) ou portal (url da tela exata da Meta, quando a API não permite). Telefone e email não têm leitura na API e vêm sempre unknown, sem bloquear. billing traz o saldo devido e o tipo de cobrança para quem anuncia na conta, e a forma de pagamento só para administradores da conta.
// @Tags			Anúncios
// @Produce		json
// @Param			id	path		string	true	"ID da conta de anúncios"
// @Success		200	{object}	ReadinessResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/ads/accounts/{id}/readiness [get]
func (h *Handler) Readiness(w http.ResponseWriter, r *http.Request) {
	readiness, err := h.d.Readiness.Readiness(r.Context(), workspaceOf(r), mux.Vars(r)["id"])
	if err != nil {
		writeError(w, err, "Failed to check the ad account")
		return
	}
	response.WriteSuccess(w, http.StatusOK, presentReadiness(readiness))
}
