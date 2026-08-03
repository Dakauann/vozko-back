package telegram

import (
	"context"
	"errors"
	"log"

	"vozko/domain/cache"
	"vozko/domain/messaging"
	tgdomain "vozko/domain/telegram"
	"vozko/domain/webhook"
	webhook_usecase "vozko/usecases/webhook"
)

// Per-topic concurrency. Conversation traffic gets the wide lane because DM
// latency is what an operator actually feels; account events are rare.
const (
	messageConcurrency = 20
	accountConcurrency = 5
)

// ConsumeWebhookUseCase subscribes the Telegram topics.
//
// Both topics carry the same unit of work, one update for one account, so they
// share the generic ConsumerRunner and differ only in concurrency.
type ConsumeWebhookUseCase struct {
	runners []interface{ Start() error }
}

func NewConsumeWebhookUseCase(
	queueSub messaging.MessageQueueSub,
	queuePub messaging.MessageQueuePub,
	sharedState cache.SharedState,
	durable tgdomain.ProcessedEventRepository,
	handler *HandleWebhookUseCase,
) *ConsumeWebhookUseCase {
	build := func(topic, name string, concurrency int) *webhook_usecase.ConsumerRunner[QueuedUpdate] {
		return webhook_usecase.NewConsumerRunner(webhook_usecase.ConsumerConfig[QueuedUpdate]{
			Name:        name,
			Topic:       topic,
			QueueSub:    queueSub,
			QueuePub:    queuePub,
			SharedState: sharedState,
			Durable:     durableAdapter{repo: durable},
			Concurrency: concurrency,
			DedupKey:    dedupKeyForUpdate,
			Handle: func(ctx context.Context, q *QueuedUpdate) error {
				return handler.Execute(ctx, q)
			},
			Classify: classifyWebhookFailure,
		})
	}

	return &ConsumeWebhookUseCase{
		runners: []interface{ Start() error }{
			build(webhook.TopicTelegramMessage, "telegram-message-webhook", messageConcurrency),
			build(webhook.TopicTelegramAccount, "telegram-account-webhook", accountConcurrency),
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

// dedupKeyForUpdate derives the idempotency key.
//
// This is markedly simpler than the Meta channels': Telegram assigns exactly one
// update_id per event, so there is no composite key to build. The account id
// scopes it because update_id is unique per BOT, not globally, two workspaces'
// bots will otherwise collide on low update ids on their first day.
func dedupKeyForUpdate(q *QueuedUpdate) string {
	if q == nil || len(q.Update) == 0 {
		return ""
	}
	update, err := tgdomain.DecodeUpdate(q.Update)
	if err != nil {
		return ""
	}
	ev := tgdomain.NormalizeUpdate(q.AccountID, update, q.Update)
	if ev == nil {
		return ""
	}
	return ev.IdempotencyKey
}

// classifyWebhookFailure decides retry vs drop vs dead-letter.
//
// The important case is per-tenant isolation: an account whose token was revoked
// fails forever, so retrying only burns the queue and delays every other
// tenant's messages behind it.
func classifyWebhookFailure(err error) webhook_usecase.Disposition {
	switch {
	case err == nil:
		return webhook_usecase.DispositionDrop

	case errors.Is(err, ErrUnknownAccount):
		// A webhook for a bot we no longer serve. Retrying can never make it
		// appear; the endpoint already 401s these, so this is the queue's tail.
		return webhook_usecase.DispositionDrop

	case errors.Is(err, tgdomain.ErrAccountNotFound),
		errors.Is(err, tgdomain.ErrInvalidUpdate):
		return webhook_usecase.DispositionDrop
	}

	if apiErr, ok := asAPIError(err); ok {
		switch {
		case apiErr.NeedsReconnect():
			log.Printf("[telegram-webhook] dropping update: bot token revoked (code=%d)", apiErr.Code)
			return webhook_usecase.DispositionDrop
		case apiErr.BlockedByUser():
			// The contact is unreachable. The inbound side has already recorded
			// what it could; retrying an outbound side effect cannot help.
			return webhook_usecase.DispositionDrop
		case apiErr.Retryable():
			return webhook_usecase.DispositionRetry
		default:
			return webhook_usecase.DispositionDeadLetter
		}
	}
	return webhook_usecase.DispositionRetry
}

// durableAdapter adapts the Telegram processed-event repository onto the generic
// runner's dedup port.
type durableAdapter struct {
	repo tgdomain.ProcessedEventRepository
}

func (d durableAdapter) Claim(ctx context.Context, key, channel, accountID string) (bool, error) {
	if d.repo == nil {
		return true, nil
	}
	return d.repo.Claim(ctx, key, channel, accountID)
}
