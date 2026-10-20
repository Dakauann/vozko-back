package tiktok

import (
	"context"
	"errors"
	"fmt"

	"vozko/domain/cache"
	"vozko/domain/messaging"
	ttdomain "vozko/domain/tiktok"
	"vozko/domain/webhook"
	webhook_usecase "vozko/usecases/webhook"
)

type EventHandler interface {
	Handle(ctx context.Context, ev ttdomain.Event) error
}

type ConsumerDeps struct {
	QueueSub    messaging.MessageQueueSub
	QueuePub    messaging.MessageQueuePub
	SharedState cache.SharedState
	Durable     webhook_usecase.DurableDedup
	Messages    EventHandler
	Comments    EventHandler
	Errors      ttdomain.ErrorClassifier
	Metrics     Metrics
	Concurrency int
}

type Consumers struct {
	runners []*webhook_usecase.ConsumerRunner[ttdomain.Envelope]
}

func NewConsumers(d ConsumerDeps) *Consumers {
	if d.QueueSub == nil || d.QueuePub == nil || d.SharedState == nil || d.Durable == nil || d.Messages == nil || d.Comments == nil || d.Errors == nil || d.Metrics == nil {
		panic("tiktok: consumers require the queue, shared state, durable dedup, both handlers, an error classifier and metrics")
	}
	classify := Classifier{Errors: d.Errors, Metrics: d.Metrics}
	build := func(name, topic string, handler EventHandler) *webhook_usecase.ConsumerRunner[ttdomain.Envelope] {
		return webhook_usecase.NewConsumerRunner(webhook_usecase.ConsumerConfig[ttdomain.Envelope]{
			Name:        name,
			Topic:       topic,
			QueueSub:    d.QueueSub,
			QueuePub:    d.QueuePub,
			SharedState: d.SharedState,
			Durable:     d.Durable,
			Concurrency: d.Concurrency,
			DedupKey:    DedupKey,
			Handle: func(ctx context.Context, env *ttdomain.Envelope) error {
				ev, err := ttdomain.Normalize(*env)
				if err != nil {
					return err
				}
				return handler.Handle(ctx, ev)
			},
			Classify: classify.Disposition,
		})
	}
	return &Consumers{runners: []*webhook_usecase.ConsumerRunner[ttdomain.Envelope]{
		build("tiktok-message-webhook", webhook.TopicTikTokMessage, d.Messages),
		build("tiktok-comment-webhook", webhook.TopicTikTokComment, d.Comments),
	}}
}

func (c *Consumers) Start() error {
	for _, r := range c.runners {
		if err := r.Start(); err != nil {
			return fmt.Errorf("tiktok: start consumer: %w", err)
		}
	}
	return nil
}

func DedupKey(env *ttdomain.Envelope) string {
	if env == nil {
		return ""
	}
	ev, err := ttdomain.Normalize(*env)
	if err != nil {
		return ""
	}
	return ev.IdempotencyKey()
}

type Classifier struct {
	Errors  ttdomain.ErrorClassifier
	Metrics Metrics
}

func (c Classifier) Disposition(err error) webhook_usecase.Disposition {
	switch {
	case errors.Is(err, ErrUnknownAccount), errors.Is(err, ErrAccountUnavailable):
		return webhook_usecase.DispositionDrop
	case errors.Is(err, ttdomain.ErrUnknownEvent), errors.Is(err, ttdomain.ErrInvalidContent), errors.Is(err, ttdomain.ErrInvalidEnvelope):
		c.Metrics.AddTikTokEvent("consume", "invalid_event")
		return webhook_usecase.DispositionDrop
	case errors.Is(err, ttdomain.ErrNeedsReconnect), c.Errors.IsAuth(err):
		c.Metrics.AddTikTokEvent("consume", "account_lost")
		return webhook_usecase.DispositionDrop
	case c.Errors.IsInvalidRequest(err):
		return webhook_usecase.DispositionDeadLetter
	}
	return webhook_usecase.DispositionRetry
}
