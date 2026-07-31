package instagram

import (
	"context"
	"errors"
	"log"

	"vozko/domain/cache"
	igdomain "vozko/domain/instagram"
	"vozko/domain/messaging"
	"vozko/domain/webhook"
	"vozko/infra/meta"
	webhook_usecase "vozko/usecases/webhook"
)

// Per-topic concurrency. Messages get the widest lane because DM latency is what
// an operator actually feels; comment moderation is not latency critical.
const (
	messageConcurrency = 20
	commentConcurrency = 10
	accountConcurrency = 5
)

// ConsumeWebhookUseCase subscribes the Instagram topics.
//
// Every topic carries the same unit of work — one entry envelope for one account
// — so all three share the generic ConsumerRunner and differ only in
// concurrency. Splitting entries at ingest is what makes this safe: a poison
// event for one tenant cannot block another's.
type ConsumeWebhookUseCase struct {
	runners []interface{ Start() error }
}

// NewConsumeWebhookUseCase wires the three Instagram consumers.
func NewConsumeWebhookUseCase(
	queueSub messaging.MessageQueueSub,
	queuePub messaging.MessageQueuePub,
	sharedState cache.SharedState,
	durable igdomain.ProcessedEventRepository,
	handler *HandleWebhookUseCase,
) *ConsumeWebhookUseCase {
	build := func(topic, name string, concurrency int) *webhook_usecase.ConsumerRunner[igdomain.EntryEnvelope] {
		return webhook_usecase.NewConsumerRunner(webhook_usecase.ConsumerConfig[igdomain.EntryEnvelope]{
			Name:        name,
			Topic:       topic,
			QueueSub:    queueSub,
			QueuePub:    queuePub,
			SharedState: sharedState,
			Durable:     durableAdapter{repo: durable},
			Concurrency: concurrency,
			DedupKey:    dedupKeyForEntry,
			Handle: func(ctx context.Context, env *igdomain.EntryEnvelope) error {
				return handler.Execute(ctx, env)
			},
			Classify: classifyWebhookFailure,
		})
	}

	return &ConsumeWebhookUseCase{
		runners: []interface{ Start() error }{
			build(webhook.TopicInstagramMessage, "instagram-message-webhook", messageConcurrency),
			build(webhook.TopicInstagramComment, "instagram-comment-webhook", commentConcurrency),
			build(webhook.TopicInstagramAccount, "instagram-account-webhook", accountConcurrency),
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

// dedupKeyForEntry derives the idempotency key for a whole entry.
//
// A single mid legitimately recurs across event kinds — the original message, its
// deletion tombstone, an edit, a read receipt and react/unreact all carry the
// same mid — so the key is composite per event. For a multi-event entry the first
// event's key represents the batch; the per-message read-before-insert in the
// history manager and the unique index on (entry_type, external_message_id)
// catch anything the batch key misses.
func dedupKeyForEntry(env *igdomain.EntryEnvelope) string {
	events := igdomain.NormalizeEntry(env)
	if len(events) == 0 {
		return ""
	}
	return events[0].IdempotencyKey
}

// classifyWebhookFailure decides retry vs drop vs dead-letter.
//
// The important case is per-tenant isolation: an account whose token has been
// revoked will fail forever, so retrying only burns the queue. Those are dropped
// (the account is already marked for reconnection), leaving retries for genuine
// transient faults.
func classifyWebhookFailure(err error) webhook_usecase.Disposition {
	switch {
	case err == nil:
		return webhook_usecase.DispositionDrop

	case errors.Is(err, ErrUnknownAccount):
		// A stale subscription for an account we no longer serve. Retrying can
		// never make it appear.
		return webhook_usecase.DispositionDrop

	case errors.Is(err, igdomain.ErrAccountNotFound),
		errors.Is(err, igdomain.ErrInvalidWebhookPayload):
		return webhook_usecase.DispositionDrop
	}

	if apiErr, ok := meta.AsError(err); ok {
		switch {
		case apiErr.NeedsReauth():
			// The tenant must reconnect; the adapter has already flagged the
			// account. Retrying would stall this account's queue lane.
			log.Printf("[instagram-webhook] dropping event: account needs reconnection (code=%d)", apiErr.Code)
			return webhook_usecase.DispositionDrop
		case apiErr.Retryable():
			return webhook_usecase.DispositionRetry
		default:
			return webhook_usecase.DispositionDeadLetter
		}
	}
	return webhook_usecase.DispositionRetry
}

// durableAdapter adapts the Instagram processed-event repository onto the generic
// runner's dedup port.
type durableAdapter struct {
	repo igdomain.ProcessedEventRepository
}

func (d durableAdapter) Claim(ctx context.Context, key, channel, accountID string) (bool, error) {
	if d.repo == nil {
		return true, nil
	}
	return d.repo.Claim(ctx, key, channel, accountID)
}
