package unofficial_whatsapp

import (
	"context"
	"errors"
	"log"

	"vozko/domain/cache"
	"vozko/domain/messaging"
	uw "vozko/domain/unofficial_whatsapp"
	"vozko/domain/webhook"
	webhook_usecase "vozko/usecases/webhook"
)

const (
	messageConcurrency  = 20
	historyConcurrency  = 4
	instanceConcurrency = 5
)

type QueuedEvent struct {
	InstanceID string `json:"instanceId"`
	Body       []byte `json:"body"`
}

var ErrUnknownInstance = errors.New("unofficial whatsapp: unknown instance")

type ConsumeWebhookUseCase struct {
	runners []interface{ Start() error }
}

func NewConsumeWebhookUseCase(
	queueSub messaging.MessageQueueSub,
	queuePub messaging.MessageQueuePub,
	sharedState cache.SharedState,
	durable uw.ProcessedEventRepository,
	handler *HandleWebhookUseCase,
) *ConsumeWebhookUseCase {
	build := func(topic, name string, concurrency int) *webhook_usecase.ConsumerRunner[QueuedEvent] {
		return webhook_usecase.NewConsumerRunner(webhook_usecase.ConsumerConfig[QueuedEvent]{
			Name:        name,
			Topic:       topic,
			QueueSub:    queueSub,
			QueuePub:    queuePub,
			SharedState: sharedState,
			Durable:     durableAdapter{repo: durable},
			Concurrency: concurrency,
			DedupKey:    dedupKeyForEvent,
			Handle: func(ctx context.Context, q *QueuedEvent) error {
				return handler.Execute(ctx, q)
			},
			Classify: classifyWebhookFailure,
		})
	}

	return &ConsumeWebhookUseCase{
		runners: []interface{ Start() error }{
			build(webhook.TopicUnofficialWhatsAppMessage, "unofficial-whatsapp-message", messageConcurrency),
			build(webhook.TopicUnofficialWhatsAppHistory, "unofficial-whatsapp-history", historyConcurrency),
			build(webhook.TopicUnofficialWhatsAppInstance, "unofficial-whatsapp-instance", instanceConcurrency),
		},
	}
}

func (uc *ConsumeWebhookUseCase) Start() error {
	for _, r := range uc.runners {
		if err := r.Start(); err != nil {
			return err
		}
	}
	return nil
}

func dedupKeyForEvent(q *QueuedEvent) string {
	if q == nil || len(q.Body) == 0 {
		return ""
	}
	env, err := uw.DecodeEnvelope(q.Body)
	if err != nil {
		return ""
	}
	events := uw.NormalizeEnvelope(q.InstanceID, env)
	if len(events) == 0 {
		return ""
	}
	return events[0].IdempotencyKey
}

func classifyWebhookFailure(err error) webhook_usecase.Disposition {
	switch {
	case err == nil:
		return webhook_usecase.DispositionDrop

	case errors.Is(err, ErrUnknownInstance),
		errors.Is(err, uw.ErrInstanceNotFound),
		errors.Is(err, uw.ErrInvalidEvent):
		return webhook_usecase.DispositionDrop
	}

	if provErr, ok := uw.AsProviderError(err); ok {
		switch {
		case provErr.NeedsReconnect():
			return webhook_usecase.DispositionDrop
		case provErr.Retryable():
			return webhook_usecase.DispositionRetry
		default:
			return webhook_usecase.DispositionDeadLetter
		}
	}

	return webhook_usecase.DispositionRetry
}

type durableAdapter struct {
	repo uw.ProcessedEventRepository
}

func (a durableAdapter) Claim(ctx context.Context, key, channel, instanceID string) (bool, error) {
	if a.repo == nil {
		log.Printf("[unofficial-whatsapp] no durable dedup store; relying on the Redis guard alone")
		return true, nil
	}
	return a.repo.Claim(ctx, key, channel, instanceID)
}
