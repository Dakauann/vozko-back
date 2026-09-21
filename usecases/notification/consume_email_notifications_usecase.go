package notification_usecase

import (
	"encoding/json"
	"log"
	"strings"
	"time"

	"vozko/domain/messaging"
	"vozko/domain/metrics"
	"vozko/domain/notification"
)

func extractErrorType(err error) string {
	if err == nil {
		return "unknown"
	}
	errStr := err.Error()

	if strings.Contains(errStr, "rate limit") || strings.Contains(errStr, "429") {
		return "rate_limit"
	}
	if strings.Contains(errStr, "timeout") || strings.Contains(errStr, "deadline") {
		return "timeout"
	}
	if strings.Contains(errStr, "connection") || strings.Contains(errStr, "refused") {
		return "connection"
	}
	if strings.Contains(errStr, "not found") || strings.Contains(errStr, "404") {
		return "not_found"
	}
	if strings.Contains(errStr, "unauthorized") || strings.Contains(errStr, "forbidden") || strings.Contains(errStr, "401") || strings.Contains(errStr, "403") {
		return "auth"
	}
	if strings.Contains(errStr, "invalid") || strings.Contains(errStr, "bad") {
		return "validation"
	}

	return "server_error"
}

func emailRetryBackoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	d := 30 * time.Second * time.Duration(1<<(attempt-1))
	if d > 5*time.Minute {
		return 5 * time.Minute
	}
	return d
}

type ConsumeEmailUseCase struct {
	subscriber     messaging.MessageQueueSub
	publisher      messaging.MessageQueuePub
	emailService   notification.EmailService
	metricsService metrics.MetricsService
	semaphore      chan struct{}
}

func NewConsumeEmailUseCase(sub messaging.MessageQueueSub, pub messaging.MessageQueuePub, emailService notification.EmailService, metricsService metrics.MetricsService) ConsumeEmailUseCase {
	return ConsumeEmailUseCase{
		subscriber:     sub,
		publisher:      pub,
		emailService:   emailService,
		metricsService: metricsService,
		semaphore:      make(chan struct{}, 10),
	}
}

func (c *ConsumeEmailUseCase) Start() error {
	err := c.subscriber.Subscribe(notification.EmailNotificationTopic, func(message []byte, ack messaging.MessageAck) {
		c.HandleEmailPublication(message, ack)
	})

	if err != nil {
		log.Printf("Failed to start email notification consumer: %v", err)
		return err
	}

	log.Println("Email notification consumer started successfully")
	return nil
}

func (ceus *ConsumeEmailUseCase) HandleEmailPublication(message []byte, ack messaging.MessageAck) {
	var notifMessage notification.NotificationQueueMessage
	if err := json.Unmarshal(message, &notifMessage); err != nil {
		log.Printf("Failed to unmarshal email notification message, dropping: %v", err)
		_ = ack.Nack(false)
		return
	}

	ceus.semaphore <- struct{}{}

	go func() {
		defer func() { <-ceus.semaphore }()
		defer func() {
			if r := recover(); r != nil {
				log.Printf("email consumer panic sending to %s: %v", notifMessage.Email, r)
				_ = ack.Nack(true)
			}
		}()

		err := ceus.emailService.SendTemplate(
			notifMessage.Email,
			notifMessage.Subject,
			notifMessage.Template,
			notifMessage.Placeholders,
		)
		if err == nil {
			_ = ack.Ack()
			return
		}

		if ceus.metricsService != nil {
			ceus.metricsService.IncEmailSendError(extractErrorType(err))
		}
		ceus.retryOrDrop(message, ack, notifMessage.Email, err)
	}()
}

func (ceus *ConsumeEmailUseCase) retryOrDrop(message []byte, ack messaging.MessageAck, recipient string, sendErr error) {
	attempt := ack.DeliveryCount()

	if ceus.publisher != nil && attempt < messaging.MaxRetries {
		delay := emailRetryBackoff(attempt)
		if perr := ceus.publisher.PublishWithDelay(notification.EmailNotificationTopic, message, delay); perr == nil {
			log.Printf("[email] send to %s failed (attempt %d/%d), retrying in %v: %v",
				recipient, attempt, messaging.MaxRetries, delay, sendErr)
			_ = ack.Ack()
			return
		} else {
			log.Printf("[email] failed to schedule delayed retry for %s (attempt %d/%d): publishErr=%v sendErr=%v",
				recipient, attempt, messaging.MaxRetries, perr, sendErr)
			_ = ack.Nack(true)
			return
		}
	}

	log.Printf("CRITICAL: [email] permanently dropping email to %s after %d attempts: %v",
		recipient, attempt, sendErr)
	_ = ack.Ack()
}

var _ notification.ConsumeEmailUseCase = &ConsumeEmailUseCase{}
