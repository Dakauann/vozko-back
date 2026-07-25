package payment_usecase

import (
	"encoding/json"
	"log"
	"runtime/debug"
	"time"
	"vozko/domain/cache"
	"vozko/domain/messaging"
	"vozko/domain/payment"
	"vozko/domain/webhook"
	webhook_usecase "vozko/usecases/webhook"
)

type consumeAsaasWebhookUseCase struct {
	queueSub  messaging.MessageQueueSub
	handler   payment.HandleAsaasWebhookUseCase
	dedup     *webhook_usecase.IdempotencyGuard
	semaphore chan struct{}
}

func NewConsumeAsaasWebhookUseCase(
	queueSub messaging.MessageQueueSub,
	handler payment.HandleAsaasWebhookUseCase,
	sharedState cache.SharedState,
) payment.ConsumeAsaasWebhookUseCase {
	return &consumeAsaasWebhookUseCase{
		queueSub:  queueSub,
		handler:   handler,
		dedup:     webhook_usecase.NewIdempotencyGuard(sharedState, 10*time.Minute),
		semaphore: make(chan struct{}, 5),
	}
}

func (uc *consumeAsaasWebhookUseCase) Start() error {
	return uc.queueSub.Subscribe(webhook.TopicAsaasPayment, func(payload []byte, ack messaging.MessageAck) {
		uc.handle(payload, ack)
	})
}

func (uc *consumeAsaasWebhookUseCase) handle(raw []byte, ack messaging.MessageAck) {
	var event payment.AsaasWebhookEvent
	if err := json.Unmarshal(raw, &event); err != nil {
		log.Printf("[webhook-consumer] invalid asaas webhook payload: %v", err)
		_ = ack.Nack(false)
		return
	}

	dedupKey := event.Payment.ID + ":" + event.Event
	if dedupKey != ":" && uc.dedup.IsDuplicate("asaas:"+dedupKey) {
		log.Printf("[webhook-consumer] duplicate asaas event ignored: %s", dedupKey)
		_ = ack.Ack()
		return
	}

	uc.semaphore <- struct{}{}
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[webhook-consumer] panic in asaas handler: %v\n%s", r, debug.Stack())
				_ = ack.Nack(false)
			}
		}()
		defer func() { <-uc.semaphore }()

		if err := uc.handler.Execute(&event); err != nil {
			log.Printf("[webhook-consumer] failed to process asaas webhook: %v", err)
			_ = ack.Nack(true)
			return
		}

		if err := ack.Ack(); err != nil {
			log.Printf("[webhook-consumer] failed to ack asaas event: %v", err)
		}
	}()
}
