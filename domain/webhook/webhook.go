package webhook

const (
	Exchange = "webhook_events_exchange"

	TopicWhatsAppMessage     = "webhook.whatsapp.message"
	TopicWhatsAppPhone       = "webhook.whatsapp.phone"
	TopicWhatsAppTemplate    = "webhook.whatsapp.template"
	TopicWhatsAppCoexistence = "webhook.whatsapp.coexistence"
	TopicAsaasPayment        = "webhook.asaas.payment"

	// Instagram topics. Split by event family so a burst of comment moderation
	// cannot delay DM delivery, and so each family can carry its own prefetch.
	//
	// TopicInstagramAccount is the catch-all: it also receives fields whose
	// payload shape Meta has not published, which are logged raw rather than
	// dropped.
	TopicInstagramMessage = "webhook.instagram.message"
	TopicInstagramComment = "webhook.instagram.comment"
	TopicInstagramAccount = "webhook.instagram.account"

	// Telegram topics. Two rather than three: Telegram has no comment surface,
	// so the split is simply "conversation traffic" versus "everything about the
	// account itself".
	//
	// TopicTelegramAccount is the catch-all. The Bot API adds new update kinds
	// several times a year, and routing an unrecognised one here means it is
	// logged rather than discarded.
	TopicTelegramMessage = "webhook.telegram.message"
	TopicTelegramAccount = "webhook.telegram.account"
)

// TelegramTopics lists every Telegram topic, for consumer registration and queue
// provisioning.
func TelegramTopics() []string {
	return []string{TopicTelegramMessage, TopicTelegramAccount}
}

// InstagramTopics lists every Instagram topic, for consumer registration and
// queue provisioning.
func InstagramTopics() []string {
	return []string{TopicInstagramMessage, TopicInstagramComment, TopicInstagramAccount}
}

// TopicForInstagramField routes a webhook field to its queue.
//
// An unrecognised field deliberately lands on the account topic instead of being
// discarded: three subscribable fields have no documented payload, and silently
// dropping them would hide real traffic.
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
