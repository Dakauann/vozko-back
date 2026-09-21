package notification_service

import (
	"log"

	"vozko/domain/notification"
)

type QueuedEmailService struct {
	publisher notification.PublishEmailUseCase
}

func NewQueuedEmailService(publisher notification.PublishEmailUseCase) *QueuedEmailService {
	return &QueuedEmailService{publisher: publisher}
}

var _ notification.EmailService = (*QueuedEmailService)(nil)

func (q *QueuedEmailService) SendTemplate(to, subject, templateName string, data map[string]interface{}) error {
	if err := q.publisher.Publish(to, subject, templateName, data); err != nil {
		log.Printf("[email] enqueue failed (template=%s, to=%s): %v", templateName, to, err)
		return err
	}
	return nil
}

func (q *QueuedEmailService) SendEmail(to, subject, body string) error {
	log.Printf("[email] raw-body SendEmail is not supported on the queued path (to=%s, subject=%s); skipped", to, subject)
	return nil
}
