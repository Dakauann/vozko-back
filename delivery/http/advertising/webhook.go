package advertisinghttp

import (
	"net/http"

	"github.com/gorilla/mux"

	"vozko/delivery/http/metawebhook"
	"vozko/domain/advertising"
	mm "vozko/domain/metamessaging"
	"vozko/domain/webhook"
)

const adAccountObject = "ad_account"

func NewAdAccountWebhookHandler(publish webhook.PublishWebhookUseCase, appSecrets []string, verifyToken string) *metawebhook.Handler {
	return metawebhook.New(metawebhook.Config{
		Name:        "meta-ads-webhook",
		Publisher:   publish,
		Secrets:     appSecrets,
		VerifyToken: verifyToken,
		Objects:     []string{adAccountObject},
		Route:       routeAdAccountEntry,
	})
}

func routeAdAccountEntry(env *mm.EntryEnvelope) []string {
	if env == nil || env.Entry == nil {
		return nil
	}
	for _, c := range env.Entry.Changes {
		if c != nil {
			return []string{webhook.TopicMetaAdAccount}
		}
	}
	return nil
}

// @Summary		Webhook de contas de anúncios da Meta
// @Description	Endpoint público da Meta para o objeto ad_account. GET responde à verificação (hub.challenge); POST recebe as mudanças, valida a assinatura X-Hub-Signature-256 e agenda a sincronização da conta.
// @Tags			Anúncios
// @Success		200	{string}	string	"recebido"
// @Failure		401	{string}	string	"assinatura inválida"
// @Failure		403	{string}	string	"token de verificação inválido"
// @Router			/webhooks/meta-ads [get]
// @Router			/webhooks/meta-ads [post]
func RegisterWebhookRoute(public *mux.Router, h *metawebhook.Handler) {
	if h == nil {
		return
	}
	public.HandleFunc(advertising.WebhookPath, h.Handle).Methods(http.MethodGet, http.MethodPost)
}
