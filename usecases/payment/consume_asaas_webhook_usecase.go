package payment_usecase

import (
	"encoding/json"
	"fmt"
	"log"
	"runtime/debug"

	billing "vozko/domain/billing"
	"vozko/domain/cache"
	"vozko/domain/messaging"
	"vozko/domain/payment"
	"vozko/domain/webhook"
)

type consumeAsaasWebhookUseCase struct {
	queueSub  messaging.MessageQueueSub
	handler   payment.HandleAsaasWebhookUseCase
	pipe      paymentEventPipe
	semaphore chan struct{}
}

func NewConsumeAsaasWebhookUseCase(
	queueSub messaging.MessageQueueSub,
	queuePub messaging.MessageQueuePub,
	handler payment.HandleAsaasWebhookUseCase,
	sharedState cache.SharedState,
	alerts billing.OpsAlerter,
) payment.ConsumeAsaasWebhookUseCase {
	return &consumeAsaasWebhookUseCase{
		queueSub:  queueSub,
		handler:   handler,
		pipe:      newPaymentEventPipe("asaas-payment-consumer", webhook.TopicAsaasPayment, queuePub, sharedState, alerts),
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
		uc.pipe.dropMalformed(raw, ack, err)
		return
	}
	event.Provider = payment.ProviderAsaas

	key := ""
	if event.Payment.ID != "" || event.Event != "" {
		key = "asaas:" + event.Payment.ID + ":" + event.Event
	}
	if !uc.pipe.claim(key, raw, ack) {
		return
	}

	uc.semaphore <- struct{}{}
	go func() {
		defer func() { <-uc.semaphore }()
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[webhook-consumer] panic in asaas handler: %v\n%s", r, debug.Stack())
				uc.pipe.failed(key, raw, ack, fmt.Errorf("panic: %v", r))
			}
		}()

		if err := uc.handler.Execute(&event); err != nil {
			uc.pipe.failed(key, raw, ack, err)
			return
		}
		uc.pipe.done(key, ack)
	}()
}
