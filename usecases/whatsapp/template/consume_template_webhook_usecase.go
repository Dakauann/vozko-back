package template_usecase

import (
	"encoding/json"
	"fmt"
	"log"
	"time"
	"vozko/domain/cache"
	"vozko/domain/messaging"
	"vozko/domain/webhook"
	whatsapptemplate "vozko/domain/whatsapp/template"
	webhook_usecase "vozko/usecases/webhook"
)

type consumeTemplateWebhookUseCase struct {
	queueSub  messaging.MessageQueueSub
	handler   whatsapptemplate.HandleTemplateWebhookUseCase
	dedup     *webhook_usecase.IdempotencyGuard
	semaphore chan struct{}
}

func NewConsumeTemplateWebhookUseCase(
	queueSub messaging.MessageQueueSub,
	handler whatsapptemplate.HandleTemplateWebhookUseCase,
	sharedState cache.SharedState,
) whatsapptemplate.ConsumeTemplateWebhookUseCase {
	return &consumeTemplateWebhookUseCase{
		queueSub:  queueSub,
		handler:   handler,
		dedup:     webhook_usecase.NewIdempotencyGuard(sharedState, 5*time.Minute),
		semaphore: make(chan struct{}, 5),
	}
}

func (uc *consumeTemplateWebhookUseCase) Start() error {
	return uc.queueSub.Subscribe(webhook.TopicWhatsAppTemplate, func(payload []byte, ack messaging.MessageAck) {
		uc.handle(payload, ack)
	})
}

func (uc *consumeTemplateWebhookUseCase) handle(raw []byte, ack messaging.MessageAck) {
	var payload whatsapptemplate.TemplateWebhookPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		log.Printf("[webhook-consumer] invalid template webhook payload: %v", err)
		_ = ack.Nack(false)
		return
	}

	dedupKey := extractTemplateDedupKey(&payload)
	if dedupKey != "" && uc.dedup.IsDuplicate("template:"+dedupKey) {
		log.Printf("[webhook-consumer] duplicate template event ignored: %s", dedupKey)
		_ = ack.Ack()
		return
	}

	uc.semaphore <- struct{}{}
	go func() {
		shouldRequeue := false
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[webhook-consumer] panic in template handler: %v", r)
				shouldRequeue = true
			}
		}()
		defer func() { <-uc.semaphore }()
		defer func() {
			if shouldRequeue {
				if err := ack.Nack(true); err != nil {
					log.Printf("[webhook-consumer] failed to nack template event: %v", err)
				}
				return
			}
			if err := ack.Ack(); err != nil {
				log.Printf("[webhook-consumer] failed to ack template event: %v", err)
			}
		}()

		if err := uc.handler.Execute(&payload); err != nil {
			log.Printf("[webhook-consumer] failed to process template event: %v", err)
			shouldRequeue = true
		}
	}()
}

func extractTemplateDedupKey(payload *whatsapptemplate.TemplateWebhookPayload) string {
	if payload == nil {
		return ""
	}
	for _, entry := range payload.Entry {
		for _, change := range entry.Changes {
			return fmt.Sprintf("%s:%d:%s:%s",
				change.Field,
				change.Value.MessageTemplateID,
				change.Value.Event,
				change.Value.MessageTemplateLanguage,
			)
		}
	}
	return ""
}
