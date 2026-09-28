package facebook

import (
	"context"
	"errors"
	"log"

	"vozko/domain/cache"
	fbdomain "vozko/domain/facebook"
	"vozko/domain/messaging"
	mm "vozko/domain/metamessaging"
	"vozko/domain/webhook"
	webhook_usecase "vozko/usecases/webhook"
)

const (
	messageConcurrency = 20
	feedConcurrency    = 10
	pageConcurrency    = 5
)

type EntryHandler interface {
	Execute(ctx context.Context, env *mm.EntryEnvelope) error
}

type ConsumeWebhookDeps struct {
	QueueSub    messaging.MessageQueueSub
	QueuePub    messaging.MessageQueuePub
	SharedState cache.SharedState
	Durable     webhook.ProcessedEventRepository
	Messages    EntryHandler
	Feed        EntryHandler
	PageEvents  EntryHandler
}

type ConsumeWebhookUseCase struct {
	runners []interface{ Start() error }
}

func NewConsumeWebhookUseCase(d ConsumeWebhookDeps) *ConsumeWebhookUseCase {
	build := func(topic, name string, concurrency int, handler EntryHandler) *webhook_usecase.ConsumerRunner[mm.EntryEnvelope] {
		return webhook_usecase.NewConsumerRunner(webhook_usecase.ConsumerConfig[mm.EntryEnvelope]{
			Name:        name,
			Topic:       topic,
			QueueSub:    d.QueueSub,
			QueuePub:    d.QueuePub,
			SharedState: d.SharedState,
			Durable:     d.Durable,
			Concurrency: concurrency,
			DedupKey:    EntryDedupKey,
			Handle:      handler.Execute,
			Classify:    ClassifyWebhookFailure,
		})
	}
	uc := &ConsumeWebhookUseCase{}
	for _, r := range []struct {
		topic, name string
		concurrency int
		handler     EntryHandler
	}{
		{webhook.TopicFacebookMessage, "facebook-message-webhook", messageConcurrency, d.Messages},
		{webhook.TopicFacebookFeed, "facebook-feed-webhook", feedConcurrency, d.Feed},
		{webhook.TopicFacebookPage, "facebook-page-webhook", pageConcurrency, d.PageEvents},
	} {
		if r.handler != nil {
			uc.runners = append(uc.runners, build(r.topic, r.name, r.concurrency, r.handler))
		}
	}
	return uc
}

func (uc *ConsumeWebhookUseCase) Start() error {
	for _, r := range uc.runners {
		if err := r.Start(); err != nil {
			return err
		}
	}
	return nil
}

func ClassifyWebhookFailure(err error) webhook_usecase.Disposition {
	if err == nil || errors.Is(err, ErrUnknownPage) || errors.Is(err, mm.ErrInvalidWebhookPayload) {
		return webhook_usecase.DispositionDrop
	}
	switch fbdomain.Classify(err) {
	case fbdomain.FailureReauth, fbdomain.FailureRoleLost:
		log.Printf("[facebook-webhook] dropping event: page needs reconnection: %v", err)
		return webhook_usecase.DispositionDrop
	case fbdomain.FailureRetryable, fbdomain.FailureUnknown:
		return webhook_usecase.DispositionRetry
	}
	return webhook_usecase.DispositionDeadLetter
}

type PageEventLogger struct{}

func (PageEventLogger) Execute(_ context.Context, env *mm.EntryEnvelope) error {
	if env == nil || env.Entry == nil {
		return nil
	}
	for _, c := range env.Entry.Changes {
		if c != nil {
			log.Printf("[facebook-webhook] page %s field %q: %s", env.Entry.ID, c.Field, truncate(c.Value, 512))
		}
	}
	return nil
}

func truncate(raw []byte, n int) string {
	if len(raw) <= n {
		return string(raw)
	}
	return string(raw[:n]) + "…"
}
