package whatsappform_usecase

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"

	"vozko/domain/cache"
	"vozko/domain/messaging"
	"vozko/domain/webhook"
	wf "vozko/domain/whatsappform"
	webhook_usecase "vozko/usecases/webhook"
)

const flowsConsumerConcurrency = 3

type FlowWebhookHandler interface {
	HandleFlowWebhook(ctx context.Context, body []byte) error
}

type FlowConsumerDeps struct {
	QueueSub    messaging.MessageQueueSub
	QueuePub    messaging.MessageQueuePub
	SharedState cache.SharedState
	Durable     webhook_usecase.DurableDedup
	Handler     FlowWebhookHandler
}

func flowDedupKey(raw *json.RawMessage) string {
	sum := sha256.Sum256(*raw)
	return "flows:" + hex.EncodeToString(sum[:])
}

func classifyFlowFailure(err error) webhook_usecase.Disposition {
	if errors.Is(err, wf.ErrFlowEventInvalid) {
		return webhook_usecase.DispositionDrop
	}
	return webhook_usecase.DispositionRetry
}

func NewFlowConsumer(d FlowConsumerDeps) *webhook_usecase.ConsumerRunner[json.RawMessage] {
	return webhook_usecase.NewConsumerRunner(webhook_usecase.ConsumerConfig[json.RawMessage]{
		Name: "whatsapp-flows-webhook", Topic: webhook.TopicWhatsAppFlows,
		QueueSub: d.QueueSub, QueuePub: d.QueuePub, SharedState: d.SharedState, Durable: d.Durable,
		Concurrency: flowsConsumerConcurrency, DedupKey: flowDedupKey, Classify: classifyFlowFailure,
		Handle: func(ctx context.Context, raw *json.RawMessage) error {
			return d.Handler.HandleFlowWebhook(ctx, *raw)
		},
	})
}
