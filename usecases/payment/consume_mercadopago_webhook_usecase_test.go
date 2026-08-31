package payment_usecase

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"vozko/domain/payment"
	"vozko/domain/webhook"
)

// stubResolver stands in for the Mercado Pago resolver. The consumer must not know
// that resolving involves an API call, only that it can succeed, be terminally
// rejected, or fail transiently.
type stubResolver struct {
	mu      sync.Mutex
	calls   int
	lastRaw []byte
	event   *payment.WebhookEvent
	err     error
	delay   time.Duration
	sawCtx  context.Context
}

func (s *stubResolver) Provider() payment.Provider { return payment.ProviderMercadoPago }

func (s *stubResolver) Resolve(ctx context.Context, raw []byte) (*payment.WebhookEvent, error) {
	s.mu.Lock()
	s.calls++
	s.lastRaw = append([]byte(nil), raw...)
	s.sawCtx = ctx
	delay := s.delay
	event, err := s.event, s.err
	s.mu.Unlock()

	if delay > 0 {
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return event, err
}

func (s *stubResolver) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func mpNotification(dataID, action string) []byte {
	return []byte(fmt.Sprintf(`{"id":112233,"type":"payment","action":%q,"data":{"id":%q}}`, action, dataID))
}

func receivedEvent(chargeID string) *payment.WebhookEvent {
	return &payment.WebhookEvent{
		ID:       "112233",
		Event:    payment.EventPaymentReceived,
		Provider: payment.ProviderMercadoPago,
		Payment: payment.WebhookPayment{
			ID:                chargeID,
			ExternalReference: "inv:abc",
			BillingType:       string(payment.MethodPix),
			Value:             49.9,
			Status:            "approved",
		},
	}
}

// waitFor polls until cond holds or the deadline passes. The consumer dispatches to a
// goroutine, so assertions cannot read state synchronously.
func waitFor(t *testing.T, cond func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return cond()
}

func newMPConsumer(sub *mockAsaasQueueSub, resolver *stubResolver, handler *mockAsaasHandler) payment.ConsumePaymentWebhookUseCase {
	return NewConsumeMercadoPagoWebhookUseCase(sub, resolver, handler, newMockAsaasSharedState())
}

func TestConsumeMercadoPago_SubscribesToItsOwnTopic(t *testing.T) {
	sub := &mockAsaasQueueSub{}
	uc := newMPConsumer(sub, &stubResolver{}, &mockAsaasHandler{})

	if err := uc.Start(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sub.subscribedTopic != webhook.TopicMercadoPagoPayment {
		t.Fatalf("topic: got %q want %q", sub.subscribedTopic, webhook.TopicMercadoPagoPayment)
	}
}

func TestConsumeMercadoPago_StartPropagatesSubscribeError(t *testing.T) {
	sub := &mockAsaasQueueSub{subscribeErr: errors.New("amqp down")}
	uc := newMPConsumer(sub, &stubResolver{}, &mockAsaasHandler{})

	if err := uc.Start(); err == nil {
		t.Fatal("expected the subscribe error to propagate")
	}
}

func TestConsumeMercadoPago_ResolvesThenHandlesThenAcks(t *testing.T) {
	sub := &mockAsaasQueueSub{}
	resolver := &stubResolver{event: receivedEvent("1234567890")}
	handler := &mockAsaasHandler{}
	uc := newMPConsumer(sub, resolver, handler)
	_ = uc.Start()

	ack := &mockAsaasAck{}
	sub.handler(mpNotification("1234567890", "payment.updated"), ack)

	if !waitFor(t, func() bool { return ack.wasAcked() }) {
		t.Fatal("expected the message to be acked")
	}
	if resolver.callCount() != 1 {
		t.Fatalf("expected one resolve, got %d", resolver.callCount())
	}
	if handler.callCount() != 1 {
		t.Fatalf("expected one handler call, got %d", handler.callCount())
	}
	got := handler.lastCall()
	if got.Event != payment.EventPaymentReceived || got.Payment.ID != "1234567890" {
		t.Fatalf("handler received the wrong event: %+v", got)
	}
	if got.Provider != payment.ProviderMercadoPago {
		t.Fatalf("provider not carried through: %q", got.Provider)
	}
	if ack.wasNacked() {
		t.Fatal("a successful message must not be nacked")
	}
}

func TestConsumeMercadoPago_MalformedEnvelopeIsDroppedNotRequeued(t *testing.T) {
	sub := &mockAsaasQueueSub{}
	resolver := &stubResolver{}
	handler := &mockAsaasHandler{}
	uc := newMPConsumer(sub, resolver, handler)
	_ = uc.Start()

	ack := &mockAsaasAck{}
	sub.handler([]byte("{not json"), ack)

	if !ack.wasNacked() {
		t.Fatal("expected a nack for an undecodable envelope")
	}
	ack.mu.Lock()
	requeued := ack.requeued
	ack.mu.Unlock()
	if requeued {
		t.Fatal("an undecodable envelope must not be requeued")
	}
	if resolver.callCount() != 0 || handler.callCount() != 0 {
		t.Fatal("nothing should run for an undecodable envelope")
	}
}

// TestConsumeMercadoPago_DedupesByResourceAndAction: Mercado Pago assigns a new
// notification id per delivery attempt, so deduping on that id would dedupe nothing.
func TestConsumeMercadoPago_DedupesByResourceAndAction(t *testing.T) {
	sub := &mockAsaasQueueSub{}
	resolver := &stubResolver{event: receivedEvent("1234567890")}
	handler := &mockAsaasHandler{}
	uc := newMPConsumer(sub, resolver, handler)
	_ = uc.Start()

	first := &mockAsaasAck{}
	sub.handler(mpNotification("1234567890", "payment.updated"), first)
	if !waitFor(t, func() bool { return handler.callCount() == 1 }) {
		t.Fatal("first delivery should reach the handler")
	}

	// Same resource + action, different notification id: must be suppressed.
	second := &mockAsaasAck{}
	sub.handler([]byte(`{"id":999999,"type":"payment","action":"payment.updated","data":{"id":"1234567890"}}`), second)

	if !waitFor(t, func() bool { return second.wasAcked() }) {
		t.Fatal("a duplicate must still be acked")
	}
	if handler.callCount() != 1 {
		t.Fatalf("duplicate reached the handler: %d calls", handler.callCount())
	}
}

func TestConsumeMercadoPago_DifferentActionIsNotADuplicate(t *testing.T) {
	sub := &mockAsaasQueueSub{}
	resolver := &stubResolver{event: receivedEvent("1234567890")}
	handler := &mockAsaasHandler{}
	uc := newMPConsumer(sub, resolver, handler)
	_ = uc.Start()

	sub.handler(mpNotification("1234567890", "payment.created"), &mockAsaasAck{})
	if !waitFor(t, func() bool { return handler.callCount() == 1 }) {
		t.Fatal("first delivery should reach the handler")
	}

	sub.handler(mpNotification("1234567890", "payment.updated"), &mockAsaasAck{})
	if !waitFor(t, func() bool { return handler.callCount() == 2 }) {
		t.Fatalf("a different action must be processed, got %d calls", handler.callCount())
	}
}

func TestConsumeMercadoPago_DifferentPaymentIsNotADuplicate(t *testing.T) {
	sub := &mockAsaasQueueSub{}
	resolver := &stubResolver{event: receivedEvent("1")}
	handler := &mockAsaasHandler{}
	uc := newMPConsumer(sub, resolver, handler)
	_ = uc.Start()

	sub.handler(mpNotification("1", "payment.updated"), &mockAsaasAck{})
	if !waitFor(t, func() bool { return handler.callCount() == 1 }) {
		t.Fatal("first delivery should reach the handler")
	}

	sub.handler(mpNotification("2", "payment.updated"), &mockAsaasAck{})
	if !waitFor(t, func() bool { return handler.callCount() == 2 }) {
		t.Fatalf("a different payment must be processed, got %d calls", handler.callCount())
	}
}

// TestConsumeMercadoPago_IgnoredIsAckedNotRetried: a non-payment topic or a payment in
// a state we do not act on is terminal; retrying would loop forever.
func TestConsumeMercadoPago_IgnoredIsAckedNotRetried(t *testing.T) {
	sub := &mockAsaasQueueSub{}
	resolver := &stubResolver{err: fmt.Errorf("%w: type \"plan\"", payment.ErrWebhookIgnored)}
	handler := &mockAsaasHandler{}
	uc := newMPConsumer(sub, resolver, handler)
	_ = uc.Start()

	ack := &mockAsaasAck{}
	sub.handler(mpNotification("1", "payment.updated"), ack)

	if !waitFor(t, func() bool { return ack.wasAcked() }) {
		t.Fatal("an ignored notification must be acked")
	}
	if ack.wasNacked() {
		t.Fatal("an ignored notification must not be nacked")
	}
	if handler.callCount() != 0 {
		t.Fatal("an ignored notification must not reach the handler")
	}
}

func TestConsumeMercadoPago_MalformedResolveIsAckedNotRetried(t *testing.T) {
	sub := &mockAsaasQueueSub{}
	resolver := &stubResolver{err: fmt.Errorf("%w: no id", payment.ErrWebhookMalformed)}
	handler := &mockAsaasHandler{}
	uc := newMPConsumer(sub, resolver, handler)
	_ = uc.Start()

	ack := &mockAsaasAck{}
	sub.handler(mpNotification("1", "payment.updated"), ack)

	if !waitFor(t, func() bool { return ack.wasAcked() }) {
		t.Fatal("a permanently malformed notification must be acked")
	}
	if handler.callCount() != 0 {
		t.Fatal("must not reach the handler")
	}
}

// TestConsumeMercadoPago_TransientResolveFailureIsRequeued: losing this message means
// losing a payment, so it must survive until the provider or network recovers.
func TestConsumeMercadoPago_TransientResolveFailureIsRequeued(t *testing.T) {
	sub := &mockAsaasQueueSub{}
	resolver := &stubResolver{err: errors.New("mercadopago: 503 service unavailable")}
	handler := &mockAsaasHandler{}
	uc := newMPConsumer(sub, resolver, handler)
	_ = uc.Start()

	ack := &mockAsaasAck{}
	sub.handler(mpNotification("1", "payment.updated"), ack)

	if !waitFor(t, func() bool { return ack.wasNacked() }) {
		t.Fatal("a transient resolve failure must be nacked")
	}
	ack.mu.Lock()
	requeued := ack.requeued
	ack.mu.Unlock()
	if !requeued {
		t.Fatal("a transient failure must be requeued, not dropped")
	}
	if ack.wasAcked() {
		t.Fatal("must not ack a transient failure")
	}
}

func TestConsumeMercadoPago_HandlerFailureIsRequeued(t *testing.T) {
	sub := &mockAsaasQueueSub{}
	resolver := &stubResolver{event: receivedEvent("1")}
	handler := &mockAsaasHandler{execErr: errors.New("database down")}
	uc := newMPConsumer(sub, resolver, handler)
	_ = uc.Start()

	ack := &mockAsaasAck{}
	sub.handler(mpNotification("1", "payment.updated"), ack)

	if !waitFor(t, func() bool { return ack.wasNacked() }) {
		t.Fatal("a handler failure must be nacked")
	}
	ack.mu.Lock()
	requeued := ack.requeued
	ack.mu.Unlock()
	if !requeued {
		t.Fatal("a handler failure must be requeued")
	}
}

func TestConsumeMercadoPago_PanicInHandlerIsContained(t *testing.T) {
	sub := &mockAsaasQueueSub{}
	resolver := &stubResolver{event: receivedEvent("1")}
	handler := &panickingHandler{}
	uc := NewConsumeMercadoPagoWebhookUseCase(sub, resolver, handler, newMockAsaasSharedState())
	_ = uc.Start()

	ack := &mockAsaasAck{}
	sub.handler(mpNotification("1", "payment.updated"), ack)

	if !waitFor(t, func() bool { return ack.wasNacked() }) {
		t.Fatal("a panic must be recovered and the message nacked, not crash the consumer")
	}
}

type panickingHandler struct{}

func (panickingHandler) Execute(*payment.WebhookEvent) error { panic("boom") }

// TestConsumeMercadoPago_ResolveIsBounded: without a deadline, one hung API call would
// hold a concurrency slot forever and eventually stall the whole consumer.
func TestConsumeMercadoPago_ResolveIsBounded(t *testing.T) {
	sub := &mockAsaasQueueSub{}
	resolver := &stubResolver{event: receivedEvent("1")}
	uc := newMPConsumer(sub, resolver, &mockAsaasHandler{})
	_ = uc.Start()

	sub.handler(mpNotification("1", "payment.updated"), &mockAsaasAck{})
	if !waitFor(t, func() bool { return resolver.callCount() == 1 }) {
		t.Fatal("resolver was not called")
	}

	resolver.mu.Lock()
	ctx := resolver.sawCtx
	resolver.mu.Unlock()
	if ctx == nil {
		t.Fatal("resolver must receive a context")
	}
	if _, ok := ctx.Deadline(); !ok {
		t.Fatal("the resolve context must carry a deadline")
	}
}

func TestConsumeMercadoPago_ProcessesConcurrentDeliveries(t *testing.T) {
	sub := &mockAsaasQueueSub{}
	resolver := &stubResolver{event: receivedEvent("1"), delay: 10 * time.Millisecond}
	handler := &mockAsaasHandler{}
	uc := newMPConsumer(sub, resolver, handler)
	_ = uc.Start()

	const n = 10
	for i := 0; i < n; i++ {
		sub.handler(mpNotification(fmt.Sprintf("%d", i), "payment.updated"), &mockAsaasAck{})
	}

	if !waitFor(t, func() bool { return handler.callCount() == n }) {
		t.Fatalf("expected %d handler calls, got %d", n, handler.callCount())
	}
}

func TestConsumeMercadoPago_EmptyResourceIDSkipsDedupButStillRuns(t *testing.T) {
	// An envelope with neither id nor action produces the degenerate ":" dedup key,
	// which is deliberately not deduped; the resolver then rejects it as malformed.
	sub := &mockAsaasQueueSub{}
	resolver := &stubResolver{err: fmt.Errorf("%w: no id", payment.ErrWebhookMalformed)}
	handler := &mockAsaasHandler{}
	uc := newMPConsumer(sub, resolver, handler)
	_ = uc.Start()

	ack := &mockAsaasAck{}
	sub.handler([]byte(`{}`), ack)

	if !waitFor(t, func() bool { return ack.wasAcked() }) {
		t.Fatal("expected an ack")
	}
	if resolver.callCount() != 1 {
		t.Fatalf("expected the resolver to be consulted, got %d calls", resolver.callCount())
	}
}

func TestConsumeMercadoPago_ForwardsRawPayloadToResolver(t *testing.T) {
	sub := &mockAsaasQueueSub{}
	resolver := &stubResolver{event: receivedEvent("1")}
	uc := newMPConsumer(sub, resolver, &mockAsaasHandler{})
	_ = uc.Start()

	raw := mpNotification("1", "payment.updated")
	sub.handler(raw, &mockAsaasAck{})

	if !waitFor(t, func() bool { return resolver.callCount() == 1 }) {
		t.Fatal("resolver was not called")
	}
	resolver.mu.Lock()
	got := string(resolver.lastRaw)
	resolver.mu.Unlock()
	if got != string(raw) {
		t.Fatalf("resolver received a modified payload:\n got: %s\nwant: %s", got, raw)
	}
}
