package shortlink_usecase

import (
	"context"
	"encoding/json"
	"fmt"

	"vozko/domain/messaging"
	"vozko/domain/shortlink"
)

type publishClickUseCase struct {
	queuePub messaging.MessageQueuePub
}

func NewPublishClickUseCase(queuePub messaging.MessageQueuePub) shortlink.PublishClickUseCase {
	return &publishClickUseCase{queuePub: queuePub}
}

func (uc *publishClickUseCase) Execute(ctx context.Context, msg shortlink.ClickMessage) error {
	if msg.Attempt <= 0 {
		msg.Attempt = 1
	}
	payload, _ := json.Marshal(msg)
	if err := uc.queuePub.Publish(shortlink.ClickTopic, payload); err != nil {
		return fmt.Errorf("failed to publish click message: %w", err)
	}
	return nil
}
