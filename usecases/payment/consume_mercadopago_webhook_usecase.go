package payment_usecase

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"runtime/debug"
	"time"

	billing "vozko/domain/billing"
	"vozko/domain/cache"
	"vozko/domain/messaging"
	"vozko/domain/payment"
	"vozko/domain/webhook"
)

const mercadoPagoResolveTimeout = 20 * time.Second

type consumeMercadoPagoWebhookUseCase struct {
	queueSub  messaging.MessageQueueSub
	resolver  payment.WebhookResolver
	handler   payment.HandlePaymentWebhookUseCase
	pipe      paymentEventPipe
	semaphore chan struct{}
}

func NewConsumeMercadoPagoWebhookUseCase(
	queueSub messaging.MessageQueueSub,
	queuePub messaging.MessageQueuePub,
	resolver payment.WebhookResolver,
	handler payment.HandlePaymentWebhookUseCase,
	sharedState cache.SharedState,
	alerts billing.OpsAlerter,
) payment.ConsumePaymentWebhookUseCase {
	return &consumeMercadoPagoWebhookUseCase{
		queueSub:  queueSub,
		resolver:  resolver,
		handler:   handler,
		pipe:      newPaymentEventPipe("mercadopago-payment-consumer", webhook.TopicMercadoPagoPayment, queuePub, sharedState, alerts),
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
		uc.pipe.dropMalformed(raw, ack, err)
		return
	}

	key := ""
	if envelope.Data.ID != "" || envelope.Action != "" {
		key = "mercadopago:" + envelope.Data.ID + ":" + envelope.Action
	}
	if !uc.pipe.claim(key, raw, ack) {
		return
	}

	uc.semaphore <- struct{}{}
	go func() {
		defer func() { <-uc.semaphore }()
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[webhook-consumer] panic in mercadopago handler: %v\n%s", r, debug.Stack())
				uc.pipe.failed(key, raw, ack, fmt.Errorf("panic: %v", r))
			}
		}()

		ctx, cancel := context.WithTimeout(context.Background(), mercadoPagoResolveTimeout)
		defer cancel()

		event, err := uc.resolver.Resolve(ctx, raw)
		if err != nil {
			if errors.Is(err, payment.ErrWebhookIgnored) || errors.Is(err, payment.ErrWebhookMalformed) {
				log.Printf("[webhook-consumer] mercadopago event dropped: %v", err)
				uc.pipe.done(key, ack)
				return
			}
			uc.pipe.failed(key, raw, ack, fmt.Errorf("resolve: %w", err))
			return
		}

		if err := uc.handler.Execute(event); err != nil {
			uc.pipe.failed(key, raw, ack, err)
			return
		}
		uc.pipe.done(key, ack)
	}()
}
