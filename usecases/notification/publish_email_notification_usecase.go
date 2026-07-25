package notification_usecase

import (
	"encoding/json"
	"vozko/domain/messaging"
	"vozko/domain/notification"
)

type PublishEmailUseCase struct {
	publisher messaging.MessageQueuePub
}

func NewPublishEmailUseCase(pub messaging.MessageQueuePub) PublishEmailUseCase {
	return PublishEmailUseCase{
		publisher: pub,
	}
}

func (p PublishEmailUseCase) Publish(userEmail string, subject string, template string, placeholders map[string]interface{}) error {
	message := notification.NotificationQueueMessage{
		Email:        userEmail,
		Template:     template,
		Subject:      subject,
		Placeholders: placeholders,
	}

	payload, err := json.Marshal(message)
	if err != nil {
		return err
	}

	if err := p.publisher.Publish(notification.EmailNotificationTopic, payload); err != nil {
		return err
		// TODO: make this pass the error to prometheus logging
	}

	return nil
}

var _ notification.PublishEmailUseCase = &PublishEmailUseCase{}
