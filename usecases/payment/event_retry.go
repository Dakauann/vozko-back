package payment_usecase

import (
	"context"
	"fmt"
	"log"
	"time"

	billing "vozko/domain/billing"
	"vozko/domain/cache"
	"vozko/domain/messaging"
	webhook_usecase "vozko/usecases/webhook"
)

const (
	retryFirstDelay   = 15 * time.Second
	retryMaxDelay     = 15 * time.Minute
	retryAlertAt      = 5
	retryCounterTTL   = 7 * 24 * time.Hour
	paymentDedupTTL   = 10 * time.Minute
	retryMaxShiftStep = 10
)

func retryDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	delay := retryFirstDelay << min(attempt-1, retryMaxShiftStep)
	return min(delay, retryMaxDelay)
}

type paymentEventPipe struct {
	name   string
	topic  string
	pub    messaging.MessageQueuePub
	state  cache.SharedState
	guard  *webhook_usecase.IdempotencyGuard
	alerts billing.OpsAlerter
}

func newPaymentEventPipe(name, topic string, pub messaging.MessageQueuePub, state cache.SharedState, alerts billing.OpsAlerter) paymentEventPipe {
	return paymentEventPipe{
		name:   name,
		topic:  topic,
		pub:    pub,
		state:  state,
		guard:  webhook_usecase.NewIdempotencyGuard(state, paymentDedupTTL),
		alerts: alerts,
	}
}

func (p paymentEventPipe) claim(key string, raw []byte, ack messaging.MessageAck) bool {
	if key == "" {
		return true
	}
	result, err := p.guard.Acquire(key)
	if err != nil {
		p.later(key, raw, ack, fmt.Errorf("dedup unavailable: %w", err))
		return false
	}
	switch result {
	case webhook_usecase.IdempotencyAcquireDuplicate:
		_ = ack.Ack()
		return false
	case webhook_usecase.IdempotencyAcquireInProgress:
		p.later(key, raw, ack, fmt.Errorf("another worker is processing %s", key))
		return false
	default:
		return true
	}
}

func (p paymentEventPipe) done(key string, ack messaging.MessageAck) {
	if key != "" {
		if err := p.guard.Complete(key); err != nil {
			log.Printf("[%s] mark %s complete: %v", p.name, key, err)
		}
		if p.state != nil {
			_ = p.state.Del(retryCounterKey(key))
		}
	}
	if err := ack.Ack(); err != nil {
		log.Printf("[%s] ack %s: %v", p.name, key, err)
	}
}

func (p paymentEventPipe) failed(key string, raw []byte, ack messaging.MessageAck, cause error) {
	if key != "" {
		if err := p.guard.Release(key); err != nil {
			log.Printf("[%s] release %s: %v", p.name, key, err)
		}
	}
	p.later(key, raw, ack, cause)
}

func (p paymentEventPipe) later(key string, raw []byte, ack messaging.MessageAck, cause error) {
	attempt := p.attempt(key)
	delay := retryDelay(attempt)
	log.Printf("[%s] payment event %s failed (attempt %d), retrying in %v: %v", p.name, key, attempt, delay, cause)
	if attempt == retryAlertAt {
		p.alert("Payment event keeps failing", fmt.Sprintf("consumer=%s event=%s attempts=%d last error: %v", p.name, key, attempt, cause))
	}
	if p.pub == nil {
		_ = ack.Nack(true)
		return
	}
	if err := p.pub.PublishWithDelay(p.topic, raw, delay); err != nil {
		log.Printf("[%s] delayed copy of %s could not be queued, requeueing the original: %v", p.name, key, err)
		_ = ack.Nack(true)
		return
	}
	_ = ack.Ack()
}

func (p paymentEventPipe) dropMalformed(raw []byte, ack messaging.MessageAck, cause error) {
	p.alert("Malformed payment event dropped", fmt.Sprintf("consumer=%s error=%v payload=%.200q", p.name, cause, raw))
	_ = ack.Nack(false)
}

func (p paymentEventPipe) attempt(key string) int {
	if key == "" || p.state == nil {
		return 1
	}
	n, err := p.state.IncrWithTTL(retryCounterKey(key), retryCounterTTL)
	if err != nil || n < 1 {
		return 1
	}
	return int(n)
}

func (p paymentEventPipe) alert(subject, detail string) {
	if p.alerts == nil {
		log.Printf("[%s] ALERT %s: %s", p.name, subject, detail)
		return
	}
	if err := p.alerts.Alert(context.Background(), subject, detail); err != nil {
		log.Printf("[%s] ALERT %s: %s (alerter failed: %v)", p.name, subject, detail, err)
	}
}

func retryCounterKey(key string) string {
	return "payment-retry:" + key
}
