package carriermessaging_usecase

import (
	"context"
	"encoding/json"
	"errors"

	"vozko/domain/cache"
	"vozko/domain/carriermessaging"
	"vozko/domain/messaging"
	"vozko/domain/rcs"
	"vozko/domain/sms"
	"vozko/domain/webhook"
	webhook_usecase "vozko/usecases/webhook"
)

const (
	statusConcurrency  = 20
	inboundConcurrency = 10
	eventConcurrency   = 5
)

func EncodeQueued(v any) ([]byte, error) {
	return json.Marshal(v)
}

func DecodeQueued(raw []byte, v any) error {
	return json.Unmarshal(raw, v)
}

func StatusDedupKey(r *carriermessaging.StatusReport) string {
	if r == nil || r.Reference == "" {
		return ""
	}
	return "carrier:status:" + r.Reference + ":" + string(r.Status) + ":" + r.ErrorCode
}

func InboundDedupKey(in *carriermessaging.Inbound) string {
	if in == nil || in.ProviderMessageID == "" {
		return ""
	}
	return "carrier:inbound:" + string(in.Channel) + ":" + in.ProviderMessageID
}

func EventDedupKey(e *carriermessaging.SuggestionClick) string {
	if e == nil || e.EventID == "" {
		return ""
	}
	return "carrier:event:" + e.EventID
}

func ClassifyWebhookFailure(err error) webhook_usecase.Disposition {
	switch {
	case err == nil,
		errors.Is(err, carriermessaging.ErrMessageNotFound),
		errors.Is(err, rcs.ErrSenderNotFound),
		errors.Is(err, sms.ErrInboundUnroutable),
		errors.Is(err, carriermessaging.ErrPhoneInvalid),
		errors.Is(err, ErrButtonUnknown),
		errors.Is(err, ErrChannelInvalid):
		return webhook_usecase.DispositionDrop
	}
	return webhook_usecase.DispositionRetry
}

type WebhookConsumers struct {
	runners []interface{ Start() error }
}

type QueueDeps struct {
	Sub         messaging.MessageQueueSub
	Pub         messaging.MessageQueuePub
	SharedState cache.SharedState
	Durable     webhook_usecase.DurableDedup
}

func NewWebhookConsumers(q QueueDeps, svc *Service) *WebhookConsumers {
	return &WebhookConsumers{runners: []interface{ Start() error }{
		webhook_usecase.NewConsumerRunner(webhook_usecase.ConsumerConfig[carriermessaging.StatusReport]{
			Name: "carrier-status-webhook", Topic: webhook.TopicCarrierStatus, QueueSub: q.Sub, QueuePub: q.Pub,
			SharedState: q.SharedState, Durable: q.Durable, Concurrency: statusConcurrency,
			DedupKey: StatusDedupKey, Classify: ClassifyWebhookFailure,
			Handle: func(ctx context.Context, r *carriermessaging.StatusReport) error { return svc.ApplyStatus(ctx, *r) },
		}),
		webhook_usecase.NewConsumerRunner(webhook_usecase.ConsumerConfig[carriermessaging.Inbound]{
			Name: "carrier-inbound-webhook", Topic: webhook.TopicCarrierInbound, QueueSub: q.Sub, QueuePub: q.Pub,
			SharedState: q.SharedState, Durable: q.Durable, Concurrency: inboundConcurrency,
			DedupKey: InboundDedupKey, Classify: ClassifyWebhookFailure,
			Handle: func(ctx context.Context, in *carriermessaging.Inbound) error { return svc.HandleInbound(ctx, *in) },
		}),
		webhook_usecase.NewConsumerRunner(webhook_usecase.ConsumerConfig[carriermessaging.SuggestionClick]{
			Name: "carrier-event-webhook", Topic: webhook.TopicCarrierEvent, QueueSub: q.Sub, QueuePub: q.Pub,
			SharedState: q.SharedState, Durable: q.Durable, Concurrency: eventConcurrency,
			DedupKey: EventDedupKey, Classify: ClassifyWebhookFailure,
			Handle: func(ctx context.Context, e *carriermessaging.SuggestionClick) error {
				return svc.HandleSuggestionClick(ctx, *e)
			},
		}),
	}}
}

func (c *WebhookConsumers) Start() error {
	for _, r := range c.runners {
		if err := r.Start(); err != nil {
			return err
		}
	}
	return nil
}
