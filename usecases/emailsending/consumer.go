package emailsending

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"vozko/domain/cache"
	es "vozko/domain/emailsending"
	"vozko/domain/messaging"
	"vozko/domain/webhook"
	"vozko/usecases/shared/delayedfire"
	webhook_usecase "vozko/usecases/webhook"
)

const (
	consumerName        = "email-provider-notifications"
	consumerConcurrency = 10
)

type QueuedNotification struct {
	MessageID  string    `json:"message_id"`
	TopicARN   string    `json:"topic_arn"`
	Message    string    `json:"message"`
	ReceivedAt time.Time `json:"received_at"`
}

func (q QueuedNotification) DedupKey() string {
	if strings.TrimSpace(q.MessageID) == "" {
		return ""
	}
	return "email-sns:" + q.MessageID
}

type NotificationIntake struct {
	publisher webhook.PublishWebhookUseCase
	now       func() time.Time
}

func NewNotificationIntake(publisher webhook.PublishWebhookUseCase) (*NotificationIntake, error) {
	if publisher == nil {
		return nil, errors.New("emailsending: the notification intake needs the webhook publisher")
	}
	return &NotificationIntake{publisher: publisher, now: func() time.Time { return time.Now().UTC() }}, nil
}

func (i *NotificationIntake) Accept(messageID, topicARN, message string) error {
	if strings.TrimSpace(messageID) == "" || strings.TrimSpace(message) == "" {
		return errors.New("emailsending: notification without id or message")
	}
	payload, err := json.Marshal(QueuedNotification{MessageID: messageID, TopicARN: topicARN, Message: message, ReceivedAt: i.now()})
	if err != nil {
		return err
	}
	return i.publisher.Publish(webhook.TopicEmailNotification, payload)
}

type ConsumerDeps struct {
	QueueSub    messaging.MessageQueueSub
	QueuePub    messaging.MessageQueuePub
	SharedState cache.SharedState
	Durable     webhook.ProcessedEventRepository
}

type Consumer struct {
	runner interface{ Start() error }
}

func (m *Module) NewConsumer(d ConsumerDeps) (*Consumer, error) {
	if d.QueueSub == nil || d.QueuePub == nil || d.SharedState == nil || d.Durable == nil {
		return nil, errors.New("emailsending: the consumer needs the queue, shared state and durable dedup")
	}
	events, decoder := m.Events, m.Events.c.Decoder
	runner := webhook_usecase.NewConsumerRunner(webhook_usecase.ConsumerConfig[QueuedNotification]{
		Name:        consumerName,
		Topic:       webhook.TopicEmailNotification,
		QueueSub:    d.QueueSub,
		QueuePub:    d.QueuePub,
		SharedState: d.SharedState,
		Durable:     d.Durable,
		Concurrency: consumerConcurrency,
		DedupKey:    func(q *QueuedNotification) string { return q.DedupKey() },
		Handle: func(ctx context.Context, q *QueuedNotification) error {
			return HandleQueued(ctx, decoder, events, *q)
		},
		Classify: ClassifyFailure,
	})
	return &Consumer{runner: runner}, nil
}

func (c *Consumer) Start() error { return c.runner.Start() }

const setupConsumerLabel = "email-setup"

func (m *Module) NewSetupConsumer(sub messaging.MessageQueueSub) (*delayedfire.Consumer, error) {
	return delayedfire.NewConsumer(sub, es.SetupFireTopic, setupConsumerLabel, m.Setup.Complete)
}

func HandleQueued(ctx context.Context, decoder es.NotificationDecoder, events *Events, q QueuedNotification) error {
	n, err := decoder.Decode([]byte(q.Message))
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUndecodableEvent, err)
	}
	return events.Apply(ctx, n)
}

func ClassifyFailure(err error) webhook_usecase.Disposition {
	switch {
	case errors.Is(err, ErrUndecodableEvent), errors.Is(err, ErrEmptyNotification):
		log.Printf("[emailsending] dropping provider notification: %v", err)
		return webhook_usecase.DispositionDrop
	}
	return webhook_usecase.DispositionRetry
}
