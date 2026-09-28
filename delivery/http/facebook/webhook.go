package facebook

import (
	"vozko/delivery/http/metawebhook"
	mm "vozko/domain/metamessaging"
	"vozko/domain/webhook"
)

func NewWebhookHandler(publish webhook.PublishWebhookUseCase, appSecrets []string, verifyToken string) *metawebhook.Handler {
	return metawebhook.New(metawebhook.Config{
		Name:        "facebook-webhook",
		Publisher:   publish,
		Secrets:     appSecrets,
		VerifyToken: verifyToken,
		Objects:     []string{"page"},
		Route:       routeEntry,
	})
}

func routeEntry(env *mm.EntryEnvelope) []string {
	var topics []string
	seen := map[string]bool{}
	add := func(topic string) {
		if !seen[topic] {
			seen[topic] = true
			topics = append(topics, topic)
		}
	}
	if len(env.Entry.Messaging) > 0 || len(env.Entry.Standby) > 0 {
		add(webhook.TopicFacebookMessage)
	}
	for _, c := range env.Entry.Changes {
		if c != nil {
			add(webhook.TopicForFacebookField(c.Field))
		}
	}
	return topics
}
