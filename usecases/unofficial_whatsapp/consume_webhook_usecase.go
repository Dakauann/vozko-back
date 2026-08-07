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

// Per-topic concurrency. Conversation traffic gets the wide lane because inbound
// latency is what an operator feels; a history backfill gets a narrow one so a
// seven-day replay cannot monopolise the workers a live customer needs.
const (
	messageConcurrency  = 20
	historyConcurrency  = 4
	instanceConcurrency = 5
)

// QueuedEvent is one webhook body awaiting processing.
//
// The RAW body is queued rather than a parsed event, so the normalizer runs in
// the consumer where a decode failure can be retried or dead-lettered, not in
// the HTTP handler where it would have to be answered synchronously.
type QueuedEvent struct {
	InstanceID string `json:"instanceId"`
	Body       []byte `json:"body"`
}

// ErrUnknownInstance means the event names an instance we no longer serve.
var ErrUnknownInstance = errors.New("unofficial whatsapp: unknown instance")

// ConsumeWebhookUseCase subscribes the channel's topics.
//
// All three carry the same unit of work — one webhook body for one instance —
// so they share the generic ConsumerRunner and differ only in concurrency.
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

// dedupKeyForEvent derives the idempotency key for a queued body.
//
// A body can normalize to SEVERAL events — a history batch is many messages in
// one delivery — so the key covers the delivery as a whole. Per-event dedup
// still happens inside the handler against the durable store, which is what
// makes a partially-processed batch safe to retry.
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
	// The first event's key identifies the delivery: a redelivery of the same
	// body produces the same first key, and a batch that genuinely differs
	// produces a different one.
	return events[0].IdempotencyKey
}

// classifyWebhookFailure decides retry vs drop vs dead-letter.
//
// The important case is per-tenant isolation: an instance that was deleted fails
// forever, so retrying only burns the queue and delays every other tenant's
// messages behind it.
func classifyWebhookFailure(err error) webhook_usecase.Disposition {
	switch {
	case err == nil:
		return webhook_usecase.DispositionDrop

	case errors.Is(err, ErrUnknownInstance),
		errors.Is(err, uw.ErrInstanceNotFound),
		errors.Is(err, uw.ErrInvalidEvent):
		// Retrying can never make the instance reappear. The endpoint already
		// 401s these, so this is the queue's tail after a deletion.
		return webhook_usecase.DispositionDrop
	}

	if provErr, ok := uw.AsProviderError(err); ok {
		switch {
		case provErr.NeedsReconnect():
			// The session is gone. The health sweep records that; replaying the
			// event would fail identically every time.
			return webhook_usecase.DispositionDrop
		case provErr.Retryable():
			return webhook_usecase.DispositionRetry
		default:
			return webhook_usecase.DispositionDeadLetter
		}
	}

	// An unclassified failure is retried: this provider has no replay endpoint,
	// so dropping on an unknown error is unrecoverable data loss, while a retry
	// is free because the pipeline is idempotent.
	return webhook_usecase.DispositionRetry
}

// durableAdapter bridges the channel's processed-event store onto the runner's
// narrow dedup port.
type durableAdapter struct {
	repo uw.ProcessedEventRepository
}

func (a durableAdapter) Claim(ctx context.Context, key, channel, instanceID string) (bool, error) {
	if a.repo == nil {
		// No durable store configured: fall back to the Redis fast path alone
		// rather than refusing every event.
		log.Printf("[unofficial-whatsapp] no durable dedup store; relying on the Redis guard alone")
		return true, nil
	}
	return a.repo.Claim(ctx, key, channel, instanceID)
}
