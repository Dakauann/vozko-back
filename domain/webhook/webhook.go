package webhook

const (
	Exchange = "webhook_events_exchange"

	TopicWhatsAppMessage     = "webhook.whatsapp.message"
	TopicWhatsAppPhone       = "webhook.whatsapp.phone"
	TopicWhatsAppTemplate    = "webhook.whatsapp.template"
	TopicWhatsAppCoexistence = "webhook.whatsapp.coexistence"
	TopicAsaasPayment        = "webhook.asaas.payment"
	TopicMercadoPagoPayment  = "webhook.mercadopago.payment"

	TopicInstagramMessage = "webhook.instagram.message"
	TopicInstagramComment = "webhook.instagram.comment"
	TopicInstagramAccount = "webhook.instagram.account"

	TopicTelegramMessage = "webhook.telegram.message"
	TopicTelegramAccount = "webhook.telegram.account"

	TopicUnofficialWhatsAppMessage  = "webhook.unofficialwhatsapp.message"
	TopicUnofficialWhatsAppHistory  = "webhook.unofficialwhatsapp.history"
	TopicUnofficialWhatsAppInstance = "webhook.unofficialwhatsapp.instance"
)

func UnofficialWhatsAppTopics() []string {
	return []string{
		TopicUnofficialWhatsAppMessage,
		TopicUnofficialWhatsAppHistory,
		TopicUnofficialWhatsAppInstance,
	}
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

func TelegramTopics() []string {
	return []string{TopicTelegramMessage, TopicTelegramAccount}
}

func InstagramTopics() []string {
	return []string{TopicInstagramMessage, TopicInstagramComment, TopicInstagramAccount}
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
