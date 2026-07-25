package webhook_usecase

import (
	"fmt"
	"vozko/domain/messaging"
	"vozko/domain/webhook"
)

type publishWebhookUseCase struct {
	queuePub messaging.MessageQueuePub
}

func NewPublishWebhookUseCase(queuePub messaging.MessageQueuePub) webhook.PublishWebhookUseCase {
	return &publishWebhookUseCase{queuePub: queuePub}
}

func (uc *publishWebhookUseCase) Publish(topic string, payload []byte) error {
	if err := uc.queuePub.Publish(topic, payload); err != nil {
		return fmt.Errorf("failed to publish webhook event to %s: %w", topic, err)
	}
	return nil
}
