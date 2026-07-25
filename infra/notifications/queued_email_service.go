package notification_service

import (
	"log"

	"vozko/domain/notification"
)

// QueuedEmailService implements notification.EmailService but ENQUEUES the email
// onto the notification queue instead of calling the email provider inline. It is
// injected into request-path senders (registration, login, password reset, email
// verification, workspace invite) so those flows never block on the provider: the
// actual provider send and its retries happen in the queue consumer. The real,
// provider-backed EmailService stays wired into the consumer.
type QueuedEmailService struct {
	publisher notification.PublishEmailUseCase
}

func NewQueuedEmailService(publisher notification.PublishEmailUseCase) *QueuedEmailService {
	return &QueuedEmailService{publisher: publisher}
}

var _ notification.EmailService = (*QueuedEmailService)(nil)

// SendTemplate enqueues a template email. The enqueue is fast and never calls the
// provider; failures (e.g. the broker is unreachable) are logged here so they are
// never silently swallowed even when a caller discards the returned error.
func (q *QueuedEmailService) SendTemplate(to, subject, templateName string, data map[string]interface{}) error {
	if err := q.publisher.Publish(to, subject, templateName, data); err != nil {
		log.Printf("[email] enqueue failed (template=%s, to=%s): %v", templateName, to, err)
		return err
	}
	return nil
}

// SendEmail has no queued equivalent: the consumer renders templates, so a raw
// body cannot be queued. It is only ever reached as a legacy fallback; rather than
// block on the provider inline, it logs and skips.
func (q *QueuedEmailService) SendEmail(to, subject, body string) error {
	log.Printf("[email] raw-body SendEmail is not supported on the queued path (to=%s, subject=%s); skipped", to, subject)
	return nil
}
