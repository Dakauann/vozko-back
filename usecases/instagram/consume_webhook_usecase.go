package instagram

import (
	"context"
	"errors"
	"log"

	"vozko/domain/cache"
	igdomain "vozko/domain/instagram"
	"vozko/domain/messaging"
	mm "vozko/domain/metamessaging"
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
	durable webhook.ProcessedEventRepository,
	handler *HandleWebhookUseCase,
) *ConsumeWebhookUseCase {
	build := func(topic, name string, concurrency int) *webhook_usecase.ConsumerRunner[mm.EntryEnvelope] {
		return webhook_usecase.NewConsumerRunner(webhook_usecase.ConsumerConfig[mm.EntryEnvelope]{
			Name:        name,
			Topic:       topic,
			QueueSub:    queueSub,
			QueuePub:    queuePub,
			SharedState: sharedState,
			Durable:     durable,
			Concurrency: concurrency,
			DedupKey:    igdomain.EntryDedupKey,
			Handle: func(ctx context.Context, env *mm.EntryEnvelope) error {
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
