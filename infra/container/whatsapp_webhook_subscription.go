package container

import (
	"log"

	"vozko/infra/meta/marketing"
	webhooksubscription_usecase "vozko/usecases/whatsapp/webhooksubscription"
)

func (c *Container) initWhatsAppWebhookSubscription() {
	gateway, err := marketing.NewGateway(marketing.Config{AppID: c.cfg.WhatsAppAppID, AppSecret: c.cfg.MetaAppSecret, GraphVersion: c.cfg.MetaAdsGraphVersion})
	if err != nil {
		log.Printf("[whatsapp] webhook subscription gateway unavailable, opt-out events depend on the app settings: %v", err)
		return
	}
	ensure := webhooksubscription_usecase.NewEnsureUseCase(gateway, c.cfg.WhatsAppWebhookVerifyToken)
	go keepWebhookSubscribed("[whatsapp] user_preferences webhook subscription", ensure.Ensure)
}
