package payment_usecase

import (
	"errors"
	"sync"
	"testing"
	"time"

	"vozko/domain/payment"
	"vozko/domain/webhook"
)

type delayedCopy struct {
	topic   string
	payload []byte
	delay   time.Duration
}

type mockQueuePub struct {
	mu      sync.Mutex
	delayed []delayedCopy
	err     error
}

func (p *mockQueuePub) Publish(string, []byte) error { return nil }
func (p *mockQueuePub) PublishWithDelay(topic string, message []byte, delay time.Duration) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.err != nil {
		return p.err
	}
	p.delayed = append(p.delayed, delayedCopy{topic: topic, payload: append([]byte(nil), message...), delay: delay})
	return nil
}
func (p *mockQueuePub) ValidateConnection() error { return nil }

func (p *mockQueuePub) copies() []delayedCopy {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]delayedCopy(nil), p.delayed...)
}

func TestRetryDelaysGrowAndStopAtTheCeiling(t *testing.T) {
	cases := map[int]time.Duration{
		1:  15 * time.Second,
		2:  30 * time.Second,
		3:  time.Minute,
		6:  8 * time.Minute,
		7:  15 * time.Minute,
		50: 15 * time.Minute,
	}
	for attempt, want := range cases {
		if got := retryDelay(attempt); got != want {
			t.Errorf("retryDelay(%d) = %v, want %v", attempt, got, want)
		}
	}
	if retryDelay(0) != 15*time.Second || retryDelay(-3) != 15*time.Second {
		t.Error("a missing attempt count must still wait the first delay")
	}
}

func asaasRig(handlerErr error) (*mockAsaasQueueSub, *mockQueuePub, *mockAsaasSharedState, *fakeAlerts, *mockAsaasHandler) {
	sub := &mockAsaasQueueSub{}
	pub := &mockQueuePub{}
	state := newMockAsaasSharedState()
	alerts := &fakeAlerts{}
	handler := &mockAsaasHandler{execErr: handlerErr}
	_ = NewConsumeAsaasWebhookUseCase(sub, pub, handler, state, alerts).Start()
	return sub, pub, state, alerts, handler
}

func TestAFailedPaymentEventIsRetriedWithADelayedCopyNotDropped(t *testing.T) {
	sub, pub, _, _, _ := asaasRig(errors.New("database down"))
	payload := makeAsaasPayload("PAYMENT_RECEIVED", "pay_fail", 10)

	ack := &mockAsaasAck{}
	sub.handler(payload, ack)

	if !waitFor(t, func() bool { return len(pub.copies()) == 1 }) {
		t.Fatal("a failed payment event must be queued again with a delay")
	}
	copy := pub.copies()[0]
	if copy.topic != webhook.TopicAsaasPayment || string(copy.payload) != string(payload) || copy.delay != retryDelay(1) {
		t.Fatalf("delayed copy = %+v", copy)
	}
	if !waitFor(t, ack.wasAcked) || ack.wasNacked() {
		t.Fatal("the original is acked only after its delayed copy is safely queued")
	}
}

func TestARetriedEventIsProcessedAgainAfterAFailure(t *testing.T) {
	sub, pub, _, _, handler := asaasRig(errors.New("database down"))
	payload := makeAsaasPayload("PAYMENT_RECEIVED", "pay_again", 10)

	sub.handler(payload, &mockAsaasAck{})
	waitFor(t, func() bool { return len(pub.copies()) == 1 })
	handler.mu.Lock()
	handler.execErr = nil
	handler.mu.Unlock()
	sub.handler(pub.copies()[0].payload, &mockAsaasAck{})

	if !waitFor(t, func() bool { return handler.callCount() == 2 }) {
		t.Fatalf("the delayed copy must reach the handler again, calls = %d", handler.callCount())
	}
}

func TestTheAttemptCountSurvivesDelayedCopiesAndAlertsOps(t *testing.T) {
	sub, pub, _, alerts, _ := asaasRig(errors.New("still down"))
	payload := makeAsaasPayload("PAYMENT_RECEIVED", "pay_stuck", 10)

	for i := 0; i < retryAlertAt; i++ {
		sub.handler(payload, &mockAsaasAck{})
		want := i + 1
		if !waitFor(t, func() bool { return len(pub.copies()) == want }) {
			t.Fatalf("attempt %d was not retried", want)
		}
	}
	copies := pub.copies()
	if copies[len(copies)-1].delay != retryDelay(retryAlertAt) {
		t.Fatalf("the delay must grow with the attempt, got %v", copies[len(copies)-1].delay)
	}
	if len(alerts.subjects) != 1 {
		t.Fatalf("ops must hear once that a payment event keeps failing, alerts = %v", alerts.subjects)
	}
}

func TestWhenTheDelayedCopyCannotBeQueuedTheOriginalIsRequeued(t *testing.T) {
	sub, pub, _, _, _ := asaasRig(errors.New("database down"))
	pub.err = errors.New("broker down")

	ack := &mockAsaasAck{}
	sub.handler(makeAsaasPayload("PAYMENT_RECEIVED", "pay_broker", 10), ack)

	if !waitFor(t, ack.wasNacked) {
		t.Fatal("expected a nack")
	}
	ack.mu.Lock()
	requeued := ack.requeued
	ack.mu.Unlock()
	if !requeued || ack.wasAcked() {
		t.Fatal("with no way to delay it, the event must go back to the queue, never be acked away")
	}
}

func TestAPanicInThePaymentHandlerIsRetriedNotDropped(t *testing.T) {
	sub := &mockAsaasQueueSub{}
	pub := &mockQueuePub{}
	_ = NewConsumeAsaasWebhookUseCase(sub, pub, &panicAsaasHandler{}, newMockAsaasSharedState(), &fakeAlerts{}).Start()

	sub.handler(makeAsaasPayload("PAYMENT_RECEIVED", "pay_panic", 10), &mockAsaasAck{})

	if !waitFor(t, func() bool { return len(pub.copies()) == 1 }) {
		t.Fatal("a panic must not lose the payment event")
	}
}

func TestADedupStoreOutageDelaysTheEventInsteadOfProcessingItBlind(t *testing.T) {
	sub, pub, state, _, handler := asaasRig(nil)
	state.setNXErr = errors.New("redis down")

	sub.handler(makeAsaasPayload("PAYMENT_RECEIVED", "pay_redis", 10), &mockAsaasAck{})

	if !waitFor(t, func() bool { return len(pub.copies()) == 1 }) {
		t.Fatal("without the dedup store the event must wait, not run")
	}
	if handler.callCount() != 0 {
		t.Fatal("the handler must not run without the dedup claim")
	}
}

func TestASucceededRetryClearsItsAttemptCount(t *testing.T) {
	sub, pub, state, _, handler := asaasRig(errors.New("down"))
	payload := makeAsaasPayload("PAYMENT_RECEIVED", "pay_clear", 10)

	sub.handler(payload, &mockAsaasAck{})
	waitFor(t, func() bool { return len(pub.copies()) == 1 })
	handler.mu.Lock()
	handler.execErr = nil
	handler.mu.Unlock()
	ack := &mockAsaasAck{}
	sub.handler(pub.copies()[0].payload, ack)
	waitFor(t, ack.wasAcked)

	state.mu.Lock()
	defer state.mu.Unlock()
	for key, n := range state.counters {
		t.Fatalf("attempt counter %s = %d survived a success", key, n)
	}
}

func TestAsaasEventsAreTaggedWithTheirProvider(t *testing.T) {
	sub, _, _, _, handler := asaasRig(nil)
	sub.handler(makeAsaasPayload("PAYMENT_RECEIVED", "pay_tag", 10), &mockAsaasAck{})

	if !waitFor(t, func() bool { return handler.callCount() == 1 }) {
		t.Fatal("expected the handler to run")
	}
	if got := handler.lastCall().Provider; got != payment.ProviderAsaas {
		t.Fatalf("provider = %q; the handler scopes invoice lookups by provider", got)
	}
}

func TestAMercadoPagoHandlerFailureIsRetriedWithADelayedCopy(t *testing.T) {
	sub := &mockAsaasQueueSub{}
	pub := &mockQueuePub{}
	resolver := &stubResolver{event: receivedEvent("1")}
	handler := &mockAsaasHandler{execErr: errors.New("database down")}
	_ = NewConsumeMercadoPagoWebhookUseCase(sub, pub, resolver, handler, newMockAsaasSharedState(), &fakeAlerts{}).Start()

	raw := mpNotification("1", "payment.updated")
	ack := &mockAsaasAck{}
	sub.handler(raw, ack)

	if !waitFor(t, func() bool { return len(pub.copies()) == 1 }) {
		t.Fatal("a failed Mercado Pago event must be queued again with a delay")
	}
	if c := pub.copies()[0]; c.topic != webhook.TopicMercadoPagoPayment || string(c.payload) != string(raw) {
		t.Fatalf("delayed copy = %+v; the raw notification is resolved again on retry", c)
	}
}
