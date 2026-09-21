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

const (
	messageConcurrency = 20
	commentConcurrency = 10
	accountConcurrency = 5
)

type ConsumeWebhookUseCase struct {
	runners []interface{ Start() error }
}

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

func dedupKeyForEntry(env *igdomain.EntryEnvelope) string {
	events := igdomain.NormalizeEntry(env)
	if len(events) == 0 {
		return ""
	}
	return events[0].IdempotencyKey
}

func classifyWebhookFailure(err error) webhook_usecase.Disposition {
	switch {
	case err == nil:
		return webhook_usecase.DispositionDrop

	case errors.Is(err, ErrUnknownAccount):
		return webhook_usecase.DispositionDrop

	case errors.Is(err, igdomain.ErrAccountNotFound),
		errors.Is(err, igdomain.ErrInvalidWebhookPayload):
		return webhook_usecase.DispositionDrop
	}

	if apiErr, ok := meta.AsError(err); ok {
		switch {
		case apiErr.NeedsReauth():
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

type durableAdapter struct {
	repo igdomain.ProcessedEventRepository
}

func (d durableAdapter) Claim(ctx context.Context, key, channel, accountID string) (bool, error) {
	if d.repo == nil {
		return true, nil
	}
	return d.repo.Claim(ctx, key, channel, accountID)
}
