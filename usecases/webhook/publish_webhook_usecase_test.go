package webhook_usecase

import (
	"errors"
	"testing"
	"time"

	"vozko/domain/messaging"
	"vozko/domain/webhook"
)

type mockQueuePub struct {
	published  []publishedMessage
	publishErr error
}

type publishedMessage struct {
	topic   string
	payload []byte
}

func (m *mockQueuePub) Publish(topic string, message []byte) error {
	if m.publishErr != nil {
		return m.publishErr
	}
	m.published = append(m.published, publishedMessage{topic: topic, payload: message})
	return nil
}

func (m *mockQueuePub) ValidateConnection() error { return nil }

func (m *mockQueuePub) PublishWithDelay(topic string, message []byte, delay time.Duration) error {
	return m.Publish(topic, message)
}

func TestPublishWebhookUseCase_PublishesCorrectTopic(t *testing.T) {
	pub := &mockQueuePub{}
	uc := NewPublishWebhookUseCase(pub)

	payload := []byte(`{"test": true}`)
	if err := uc.Publish(webhook.TopicAsaasPayment, payload); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(pub.published) != 1 {
		t.Fatalf("expected 1 published message, got %d", len(pub.published))
	}
	if pub.published[0].topic != webhook.TopicAsaasPayment {
		t.Fatalf("expected topic %s, got %s", webhook.TopicAsaasPayment, pub.published[0].topic)
	}
	if string(pub.published[0].payload) != `{"test": true}` {
		t.Fatalf("payload mismatch: %s", pub.published[0].payload)
	}
}

func TestPublishWebhookUseCase_AllTopics(t *testing.T) {
	topics := []string{
		webhook.TopicWhatsAppMessage,
		webhook.TopicWhatsAppPhone,
		webhook.TopicWhatsAppTemplate,
		webhook.TopicAsaasPayment,
	}

	for _, topic := range topics {
		pub := &mockQueuePub{}
		uc := NewPublishWebhookUseCase(pub)

		if err := uc.Publish(topic, []byte(`{}`)); err != nil {
			t.Fatalf("topic %s: unexpected error: %v", topic, err)
		}
		if pub.published[0].topic != topic {
			t.Fatalf("topic mismatch: expected %s, got %s", topic, pub.published[0].topic)
		}
	}
}

func TestPublishWebhookUseCase_QueueError(t *testing.T) {
	pub := &mockQueuePub{publishErr: errors.New("connection lost")}
	uc := NewPublishWebhookUseCase(pub)

	err := uc.Publish(webhook.TopicAsaasPayment, []byte(`{}`))
	if err == nil {
		t.Fatal("expected error when queue fails")
	}

	var target *messaging.MessageQueuePub
	_ = target
}

func TestPublishWebhookUseCase_ImplementsInterface(t *testing.T) {
	pub := &mockQueuePub{}
	var _ webhook.PublishWebhookUseCase = NewPublishWebhookUseCase(pub)
}
