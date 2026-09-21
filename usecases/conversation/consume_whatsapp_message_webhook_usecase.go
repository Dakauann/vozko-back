package conversation_usecase

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"
	"vozko/domain/cache"
	"vozko/domain/conversation"
	"vozko/domain/messaging"
	"vozko/domain/webhook"
	webhook_usecase "vozko/usecases/webhook"
)

type floodDetectedKeyType struct{}

var floodDetectedKey = floodDetectedKeyType{}

func WithFloodDetected(ctx context.Context) context.Context {
	return context.WithValue(ctx, floodDetectedKey, true)
}

func IsFloodDetected(ctx context.Context) bool {
	v, _ := ctx.Value(floodDetectedKey).(bool)
	return v
}

const (
	floodWindow = 10 * time.Second

	floodThreshold                   int64 = 20
	whatsAppInProgressRetryBaseDelay       = 2 * time.Second
	whatsAppInProgressRetryMaxDelay        = 30 * time.Second

	whatsAppRetryCounterTTL = 10 * time.Minute

	senderInFlightTTL = 3 * time.Minute

	maxConcurrentHandlers = 20
)

type consumeWhatsAppMessageWebhookUseCase struct {
	queueSub    messaging.MessageQueueSub
	queuePub    messaging.MessageQueuePub
	handler     conversation.HandleWhatsAppMessageUseCase
	dedup       *webhook_usecase.IdempotencyGuard
	sharedState cache.SharedState
	semaphore   chan struct{}
}

func NewConsumeWhatsAppMessageWebhookUseCase(
	queueSub messaging.MessageQueueSub,
	handler conversation.HandleWhatsAppMessageUseCase,
	sharedState cache.SharedState,
) conversation.ConsumeWhatsAppMessageWebhookUseCase {
	return NewConsumeWhatsAppMessageWebhookUseCaseWithPublisher(queueSub, nil, handler, sharedState)
}

func NewConsumeWhatsAppMessageWebhookUseCaseWithPublisher(
	queueSub messaging.MessageQueueSub,
	queuePub messaging.MessageQueuePub,
	handler conversation.HandleWhatsAppMessageUseCase,
	sharedState cache.SharedState,
) conversation.ConsumeWhatsAppMessageWebhookUseCase {
	return &consumeWhatsAppMessageWebhookUseCase{
		queueSub:    queueSub,
		queuePub:    queuePub,
		handler:     handler,
		dedup:       webhook_usecase.NewIdempotencyGuard(sharedState, 5*time.Minute),
		sharedState: sharedState,
		semaphore:   make(chan struct{}, maxConcurrentHandlers),
	}
}

func (uc *consumeWhatsAppMessageWebhookUseCase) Start() error {
	return uc.queueSub.Subscribe(webhook.TopicWhatsAppMessage, func(payload []byte, ack messaging.MessageAck) {
		uc.handle(payload, ack)
	})
}

func (uc *consumeWhatsAppMessageWebhookUseCase) handle(raw []byte, ack messaging.MessageAck) {
	var payload conversation.WhatsAppWebhookPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		log.Printf("[webhook-consumer] invalid whatsapp message payload: %v", err)
		_ = ack.Nack(false)
		return
	}

	dedupKey, shouldDedup := extractWhatsAppDedupKey(&payload)
	trackedDedupKey := ""
	if shouldDedup && dedupKey != "" {
		trackedDedupKey = "wa:" + dedupKey
		result, err := uc.dedup.Acquire(trackedDedupKey)
		if err != nil {
			log.Printf("[webhook-consumer] failed to acquire whatsapp dedupe key %s: %v", dedupKey, err)
			_ = ack.Nack(true)
			return
		}
		switch result {
		case webhook_usecase.IdempotencyAcquireDuplicate:
			log.Printf("[webhook-consumer] duplicate whatsapp message ignored: %s", dedupKey)
			_ = ack.Ack()
			return
		case webhook_usecase.IdempotencyAcquireInProgress:
			if uc.requeueInProgressMessage(raw, ack, dedupKey) {
				return
			}
			log.Printf("[webhook-consumer] whatsapp message already in progress, requeueing immediately: %s", dedupKey)
			_ = ack.Nack(true)
			return
		}
	}

	sender := extractSenderPhone(&payload)
	serialKey := sender
	if serialKey == "" {
		serialKey = extractWhatsAppMessageID(&payload)
	}
	inflightKey := ""
	if serialKey != "" {
		inflightKey = "inflight:wa:" + serialKey
		acquired, lockErr := uc.sharedState.SetNX(inflightKey, "1", senderInFlightTTL)
		if lockErr != nil {
			log.Printf("[webhook-consumer] in-flight lock error for %s: %v, proceeding without lock", serialKey, lockErr)
			inflightKey = ""
		} else if !acquired {
			if trackedDedupKey != "" {
				if releaseErr := uc.dedup.Release(trackedDedupKey); releaseErr != nil {
					log.Printf("[webhook-consumer] failed to release dedupe key on in-flight requeue for %s: %v", serialKey, releaseErr)
				}
			}
			if uc.requeueInProgressMessage(raw, ack, dedupKey) {
				return
			}
			log.Printf("[webhook-consumer] %s busy, requeueing immediately: %s", serialKey, dedupKey)
			_ = ack.Nack(true)
			return
		}
	}

	flood := false
	if sender != "" {
		key := fmt.Sprintf("flood:wa:%s", sender)
		count, err := uc.sharedState.IncrWithTTL(key, floodWindow)
		if err != nil {
			log.Printf("[webhook-consumer] flood check error for %s: %v", sender, err)
		} else if count > floodThreshold {
			log.Printf("[webhook-consumer] flood detected for %s (%d msgs in %v), skipping AI", sender, count, floodWindow)
			flood = true
		}
	}

	uc.semaphore <- struct{}{}
	go func() {
		defer func() {
			if inflightKey != "" {
				if err := uc.sharedState.Del(inflightKey); err != nil {
					log.Printf("[webhook-consumer] failed to release in-flight lock %s: %v", inflightKey, err)
				}
			}
		}()
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[webhook-consumer] panic in whatsapp message handler: %v", r)
				if trackedDedupKey != "" {
					if err := uc.dedup.Release(trackedDedupKey); err != nil {
						log.Printf("[webhook-consumer] failed to release whatsapp dedupe key after panic: %v", err)
					}
				}
				_ = ack.Nack(true)
			}
		}()
		defer func() { <-uc.semaphore }()

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()

		if flood {
			ctx = WithFloodDetected(ctx)
		}

		if err := uc.handler.Execute(ctx, &payload); err != nil {
			if trackedDedupKey != "" {
				if releaseErr := uc.dedup.Release(trackedDedupKey); releaseErr != nil {
					log.Printf("[webhook-consumer] failed to release whatsapp dedupe key after handler error: %v", releaseErr)
				}
			}
			if !errors.Is(err, conversation.ErrWhatsAppWebhookSkipped) {

				uc.scheduleWhatsAppRetry(raw, ack, dedupKey, err)
				return
			}
			if err := ack.Ack(); err != nil {
				log.Printf("[webhook-consumer] failed to ack skipped whatsapp message: %v", err)
			}
			return
		}

		if trackedDedupKey != "" {
			if err := uc.dedup.Complete(trackedDedupKey); err != nil {
				log.Printf("[webhook-consumer] failed to complete whatsapp dedupe key %s: %v", trackedDedupKey, err)
			}
		}

		if err := ack.Ack(); err != nil {
			log.Printf("[webhook-consumer] failed to ack whatsapp message: %v", err)
		}
	}()
}

func (uc *consumeWhatsAppMessageWebhookUseCase) requeueInProgressMessage(raw []byte, ack messaging.MessageAck, dedupKey string) bool {
	if uc.queuePub == nil {
		return false
	}

	delay := whatsAppInProgressRetryDelay(ack.DeliveryCount())
	if err := uc.queuePub.PublishWithDelay(webhook.TopicWhatsAppMessage, raw, delay); err != nil {
		log.Printf("[webhook-consumer] failed to delay whatsapp message retry for %s: %v", dedupKey, err)
		return false
	}

	log.Printf("[webhook-consumer] whatsapp message already in progress, delaying retry by %v: %s", delay, dedupKey)
	if err := ack.Ack(); err != nil {
		log.Printf("[webhook-consumer] failed to ack delayed whatsapp retry for %s: %v", dedupKey, err)
	}
	return true
}

func (uc *consumeWhatsAppMessageWebhookUseCase) scheduleWhatsAppRetry(raw []byte, ack messaging.MessageAck, dedupKey string, handlerErr error) {
	if uc.queuePub == nil {
		log.Printf("[webhook-consumer] dropping failed whatsapp message: no delay publisher configured (dedup=%s): %v", dedupKey, handlerErr)
		uc.ackOrLog(ack, dedupKey, "drop-no-publisher")
		return
	}
	if dedupKey == "" {
		log.Printf("[webhook-consumer] dropping failed whatsapp message: no stable dedup key, cannot bound retries: %v", handlerErr)
		uc.ackOrLog(ack, dedupKey, "drop-no-dedup-key")
		return
	}

	attempt, err := uc.incrementRetryCounter(dedupKey)
	if err != nil {

		log.Printf("[webhook-consumer] retry counter unavailable for %s, dropping to be safe: %v (handlerErr=%v)", dedupKey, err, handlerErr)
		uc.ackOrLog(ack, dedupKey, "drop-counter-unavailable")
		return
	}

	if attempt >= int64(messaging.MaxRetries) {
		log.Printf("[webhook-consumer] permanently dropping whatsapp message after %d attempts (dedup=%s): %v", attempt, dedupKey, handlerErr)
		uc.clearRetryCounter(dedupKey)
		uc.ackOrLog(ack, dedupKey, "drop-max-retries")
		return
	}

	delay := whatsAppRetryBackoff(int(attempt))
	if err := uc.queuePub.PublishWithDelay(webhook.TopicWhatsAppMessage, raw, delay); err != nil {

		log.Printf("[webhook-consumer] failed to schedule delayed retry for %s, dropping (attempt %d/%d): publishErr=%v handlerErr=%v",
			dedupKey, attempt, messaging.MaxRetries, err, handlerErr)
		uc.ackOrLog(ack, dedupKey, "drop-publish-error")
		return
	}

	log.Printf("[webhook-consumer] whatsapp message handler failed (attempt %d/%d), retrying in %v (dedup=%s): %v",
		attempt, messaging.MaxRetries, delay, dedupKey, handlerErr)
	uc.ackOrLog(ack, dedupKey, "retry-scheduled")
}

func (uc *consumeWhatsAppMessageWebhookUseCase) incrementRetryCounter(dedupKey string) (int64, error) {
	if uc.sharedState == nil {
		return 0, errors.New("no shared state configured")
	}
	key := "wa:retry:" + dedupKey
	return uc.sharedState.IncrWithTTL(key, whatsAppRetryCounterTTL)
}

func (uc *consumeWhatsAppMessageWebhookUseCase) clearRetryCounter(dedupKey string) {
	if uc.sharedState == nil || dedupKey == "" {
		return
	}
	key := "wa:retry:" + dedupKey
	if err := uc.sharedState.Del(key); err != nil {
		log.Printf("[webhook-consumer] failed to clear retry counter for %s: %v", dedupKey, err)
	}
}

func (uc *consumeWhatsAppMessageWebhookUseCase) ackOrLog(ack messaging.MessageAck, dedupKey, reason string) {
	if err := ack.Ack(); err != nil {
		log.Printf("[webhook-consumer] failed to ack whatsapp message (dedup=%s, reason=%s): %v", dedupKey, reason, err)
	}
}

func whatsAppRetryBackoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	delay := whatsAppInProgressRetryBaseDelay
	for i := 1; i < attempt; i++ {
		if delay >= whatsAppInProgressRetryMaxDelay/2 {
			return whatsAppInProgressRetryMaxDelay
		}
		delay *= 2
	}
	if delay > whatsAppInProgressRetryMaxDelay {
		return whatsAppInProgressRetryMaxDelay
	}
	return delay
}

func whatsAppInProgressRetryDelay(deliveryCount int) time.Duration {
	if deliveryCount < 1 {
		deliveryCount = 1
	}

	delay := whatsAppInProgressRetryBaseDelay
	for attempt := 1; attempt < deliveryCount; attempt++ {
		if delay >= whatsAppInProgressRetryMaxDelay/2 {
			return whatsAppInProgressRetryMaxDelay
		}
		delay *= 2
	}

	if delay > whatsAppInProgressRetryMaxDelay {
		return whatsAppInProgressRetryMaxDelay
	}
	return delay
}

func extractWhatsAppDedupKey(payload *conversation.WhatsAppWebhookPayload) (string, bool) {
	if payload == nil {
		return "", false
	}
	for _, entry := range payload.Entry {
		for _, change := range entry.Changes {
			for _, status := range change.Value.Statuses {
				if strings.EqualFold(strings.TrimSpace(status.Status), "failed") {
					return "", false
				}
			}
		}
	}

	key := extractWhatsAppMessageID(payload)
	return key, key != ""
}

func extractWhatsAppMessageID(payload *conversation.WhatsAppWebhookPayload) string {
	if payload == nil {
		return ""
	}
	for _, entry := range payload.Entry {
		for _, change := range entry.Changes {
			for _, msg := range change.Value.Messages {
				if msg.Audio != nil && msg.Audio.ID != "" {
					return "audio:" + msg.Audio.ID
				}
				if msg.ID != "" {
					return "msg:" + msg.ID
				}
			}
			for _, status := range change.Value.Statuses {
				if status.ID != "" {
					return "status:" + status.ID + ":" + status.Status
				}
			}
		}
	}
	return ""
}

func extractSenderPhone(payload *conversation.WhatsAppWebhookPayload) string {
	if payload == nil {
		return ""
	}
	for _, entry := range payload.Entry {
		for _, change := range entry.Changes {
			for _, msg := range change.Value.Messages {
				if msg.From != "" {
					return msg.From
				}
			}
		}
	}
	return ""
}
