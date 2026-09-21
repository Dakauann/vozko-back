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

const (
	messageConcurrency = 20
	accountConcurrency = 5
)

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

func classifyWebhookFailure(err error) webhook_usecase.Disposition {
	switch {
	case err == nil:
		return webhook_usecase.DispositionDrop

	case errors.Is(err, ErrUnknownAccount):
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
			return webhook_usecase.DispositionDrop
		case apiErr.Retryable():
			return webhook_usecase.DispositionRetry
		default:
			return webhook_usecase.DispositionDeadLetter
		}
	}
	return webhook_usecase.DispositionRetry
}

type durableAdapter struct {
	repo tgdomain.ProcessedEventRepository
}

func (d durableAdapter) Claim(ctx context.Context, key, channel, accountID string) (bool, error) {
	if d.repo == nil {
		return true, nil
	}
	return d.repo.Claim(ctx, key, channel, accountID)
}
