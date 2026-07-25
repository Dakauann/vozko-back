package webhook

const (
	Exchange = "webhook_events_exchange"

	TopicWhatsAppMessage     = "webhook.whatsapp.message"
	TopicWhatsAppPhone       = "webhook.whatsapp.phone"
	TopicWhatsAppTemplate    = "webhook.whatsapp.template"
	TopicWhatsAppCoexistence = "webhook.whatsapp.coexistence"
	TopicAsaasPayment        = "webhook.asaas.payment"
)

type PublishWebhookUseCase interface {
	Publish(topic string, payload []byte) error
}
