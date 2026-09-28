package instagram

import (
	"vozko/delivery/http/metawebhook"
	mm "vozko/domain/metamessaging"
	"vozko/domain/webhook"
)

type WebhookHandler = metawebhook.Handler

func NewWebhookHandler(
	publish webhook.PublishWebhookUseCase,
	appSecrets []string,
	verifyToken string,
) *WebhookHandler {
	return metawebhook.New(metawebhook.Config{
		Name:        "instagram-webhook",
		Publisher:   publish,
		Secrets:     appSecrets,
		VerifyToken: verifyToken,
		Route:       routeEntry,
	})
}

func routeEntry(env *mm.EntryEnvelope) []string {
	return []string{webhook.TopicForInstagramField(fieldOf(env))}
}

func fieldOf(env *mm.EntryEnvelope) string {
	e := env.Entry
	switch {
	case len(e.Messaging) > 0:
		return "messages"
	case len(e.Standby) > 0:
		return "standby"
	case e.Field != "":
		return e.Field
	case len(e.Changes) > 0:
		for _, c := range e.Changes {
			if c != nil && c.Field != "" {
				return c.Field
			}
		}
	}
	return ""
}
