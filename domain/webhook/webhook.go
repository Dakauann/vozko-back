package webhook

const (
	Exchange = "webhook_events_exchange"

	TopicWhatsAppMessage     = "webhook.whatsapp.message"
	TopicWhatsAppPhone       = "webhook.whatsapp.phone"
	TopicWhatsAppTemplate    = "webhook.whatsapp.template"
	TopicWhatsAppCoexistence = "webhook.whatsapp.coexistence"
	TopicWhatsAppFlows       = "webhook.whatsapp.flows"
	TopicAsaasPayment        = "webhook.asaas.payment"
	TopicMercadoPagoPayment  = "webhook.mercadopago.payment"

	TopicInstagramMessage = "webhook.instagram.message"
	TopicInstagramComment = "webhook.instagram.comment"
	TopicInstagramAccount = "webhook.instagram.account"

	TopicFacebookMessage = "webhook.facebook.message"
	TopicFacebookFeed    = "webhook.facebook.feed"
	TopicFacebookPage    = "webhook.facebook.page"
	TopicFacebookLeadgen = "webhook.facebook.leadgen"

	TopicMetaAdAccount = "webhook.meta.adaccount"

	TopicTelegramMessage = "webhook.telegram.message"
	TopicTelegramAccount = "webhook.telegram.account"

	TopicMercadoLivreMessage  = "webhook.mercadolivre.message"
	TopicMercadoLivreQuestion = "webhook.mercadolivre.question"
	TopicMercadoLivreOrder    = "webhook.mercadolivre.order"

	TopicTikTokMessage = "webhook.tiktok.message"
	TopicTikTokComment = "webhook.tiktok.comment"

	TopicEmailNotification = "webhook.email.notification"

	TopicCarrierStatus  = "webhook.carrier.status"
	TopicCarrierInbound = "webhook.carrier.inbound"
	TopicCarrierEvent   = "webhook.carrier.event"

	TopicUnofficialWhatsAppMessage  = "webhook.unofficialwhatsapp.message"
	TopicUnofficialWhatsAppHistory  = "webhook.unofficialwhatsapp.history"
	TopicUnofficialWhatsAppInstance = "webhook.unofficialwhatsapp.instance"
)

func IsWhatsAppFlowsField(field string) bool {
	return field == "flows"
}

func TopicForUnofficialWhatsAppEvent(event string) string {
	switch event {
	case "messages", "messages_update":
		return TopicUnofficialWhatsAppMessage
	case "history":
		return TopicUnofficialWhatsAppHistory
	default:
		return TopicUnofficialWhatsAppInstance
	}
}

func TopicForFacebookField(field string) string {
	switch field {
	case "feed", "mention", "videos":
		return TopicFacebookFeed
	case "leadgen":
		return TopicFacebookLeadgen
	default:
		return TopicFacebookPage
	}
}

func TopicForInstagramField(field string) string {
	switch field {
	case "comments", "live_comments", "mentions":
		return TopicInstagramComment
	case "messages", "message_echoes", "message_reactions", "messaging_seen",
		"messaging_postbacks", "messaging_referral", "messaging_optins",
		"messaging_handover", "standby", "message_edit":
		return TopicInstagramMessage
	default:
		return TopicInstagramAccount
	}
}

type PublishWebhookUseCase interface {
	Publish(topic string, payload []byte) error
}
