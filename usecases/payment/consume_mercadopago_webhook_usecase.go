package payment_usecase

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"runtime/debug"
	"time"

	"vozko/domain/cache"
	"vozko/domain/messaging"
	"vozko/domain/payment"
	"vozko/domain/webhook"
	webhook_usecase "vozko/usecases/webhook"
)

const mercadoPagoResolveTimeout = 20 * time.Second

type consumeMercadoPagoWebhookUseCase struct {
	queueSub  messaging.MessageQueueSub
	resolver  payment.WebhookResolver
	handler   payment.HandlePaymentWebhookUseCase
	dedup     *webhook_usecase.IdempotencyGuard
	semaphore chan struct{}
}

func NewConsumeMercadoPagoWebhookUseCase(
	queueSub messaging.MessageQueueSub,
	resolver payment.WebhookResolver,
	handler payment.HandlePaymentWebhookUseCase,
	sharedState cache.SharedState,
) payment.ConsumePaymentWebhookUseCase {
	return &consumeMercadoPagoWebhookUseCase{
		queueSub:  queueSub,
		resolver:  resolver,
		handler:   handler,
		dedup:     webhook_usecase.NewIdempotencyGuard(sharedState, 10*time.Minute),
		semaphore: make(chan struct{}, 5),
	}
}

func (uc *consumeMercadoPagoWebhookUseCase) Start() error {
	return uc.queueSub.Subscribe(webhook.TopicMercadoPagoPayment, func(payload []byte, ack messaging.MessageAck) {
		uc.handle(payload, ack)
	})
}

type notificationEnvelope struct {
	Action string `json:"action"`
	Type   string `json:"type"`
	Data   struct {
		ID string `json:"id"`
	} `json:"data"`
}

func (uc *consumeMercadoPagoWebhookUseCase) handle(raw []byte, ack messaging.MessageAck) {
	var envelope notificationEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		log.Printf("[webhook-consumer] invalid mercadopago webhook payload: %v", err)
		_ = ack.Nack(false)
		return
	}

	dedupKey := envelope.Data.ID + ":" + envelope.Action
	if dedupKey != ":" && uc.dedup.IsDuplicate("mercadopago:"+dedupKey) {
		log.Printf("[webhook-consumer] duplicate mercadopago event ignored: %s", dedupKey)
		_ = ack.Ack()
		return
	}

	uc.semaphore <- struct{}{}
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[webhook-consumer] panic in mercadopago handler: %v\n%s", r, debug.Stack())
				_ = ack.Nack(false)
			}
		}()
		defer func() { <-uc.semaphore }()

		ctx, cancel := context.WithTimeout(context.Background(), mercadoPagoResolveTimeout)
		defer cancel()

		event, err := uc.resolver.Resolve(ctx, raw)
		if err != nil {
			if errors.Is(err, payment.ErrWebhookIgnored) || errors.Is(err, payment.ErrWebhookMalformed) {
				log.Printf("[webhook-consumer] mercadopago event dropped: %v", err)
				_ = ack.Ack()
				return
			}
			log.Printf("[webhook-consumer] failed to resolve mercadopago webhook: %v", err)
			_ = ack.Nack(true)
			return
		}

		if err := uc.handler.Execute(event); err != nil {
			log.Printf("[webhook-consumer] failed to process mercadopago webhook: %v", err)
			_ = ack.Nack(true)
			return
		}

		if err := ack.Ack(); err != nil {
			log.Printf("[webhook-consumer] failed to ack mercadopago event: %v", err)
		}
	}()
}
