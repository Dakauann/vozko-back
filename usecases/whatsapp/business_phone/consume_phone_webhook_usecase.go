package businessphone_usecase

import (
	"encoding/json"
	"fmt"
	"log"
	"time"
	"vozko/domain/cache"
	"vozko/domain/messaging"
	"vozko/domain/webhook"
	businessphone "vozko/domain/whatsapp/business_phone"
	webhook_usecase "vozko/usecases/webhook"
)

type consumePhoneWebhookUseCase struct {
	queueSub  messaging.MessageQueueSub
	handler   businessphone.HandlePhoneWebhookUseCase
	dedup     *webhook_usecase.IdempotencyGuard
	semaphore chan struct{}
}

func NewConsumePhoneWebhookUseCase(
	queueSub messaging.MessageQueueSub,
	handler businessphone.HandlePhoneWebhookUseCase,
	sharedState cache.SharedState,
) businessphone.ConsumePhoneWebhookUseCase {
	return &consumePhoneWebhookUseCase{
		queueSub:  queueSub,
		handler:   handler,
		dedup:     webhook_usecase.NewIdempotencyGuard(sharedState, 5*time.Minute),
		semaphore: make(chan struct{}, 5),
	}
}

func (uc *consumePhoneWebhookUseCase) Start() error {
	return uc.queueSub.Subscribe(webhook.TopicWhatsAppPhone, func(payload []byte, ack messaging.MessageAck) {
		uc.handle(payload, ack)
	})
}

func (uc *consumePhoneWebhookUseCase) handle(raw []byte, ack messaging.MessageAck) {
	var payload businessphone.PhoneWebhookPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		log.Printf("[webhook-consumer] invalid phone webhook payload: %v", err)
		_ = ack.Nack(false)
		return
	}

	dedupKey := extractPhoneDedupKey(&payload)
	if dedupKey != "" && uc.dedup.IsDuplicate("phone:"+dedupKey) {
		log.Printf("[webhook-consumer] duplicate phone event ignored: %s", dedupKey)
		_ = ack.Ack()
		return
	}

	uc.semaphore <- struct{}{}
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[webhook-consumer] panic in phone handler: %v", r)
			}
		}()
		defer func() { <-uc.semaphore }()
		defer func() {
			if err := ack.Ack(); err != nil {
				log.Printf("[webhook-consumer] failed to ack phone event: %v", err)
			}
		}()

		if err := uc.handler.Execute(&payload); err != nil {
			log.Printf("[webhook-consumer] failed to process phone event: %v", err)
		}
	}()
}

func extractPhoneDedupKey(payload *businessphone.PhoneWebhookPayload) string {
	if payload == nil {
		return ""
	}
	for _, entry := range payload.Entry {
		for _, change := range entry.Changes {
			return fmt.Sprintf("%s:%s:%s", entry.ID, change.Field, change.Value.DisplayPhoneNumber)
		}
	}
	return ""
}
