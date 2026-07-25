package payment_usecase

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"vozko/domain/messaging"
	"vozko/domain/payment"
	"vozko/domain/webhook"
)

type mockAsaasQueueSub struct {
	subscribedTopic string
	handler         func([]byte, messaging.MessageAck)
	subscribeErr    error
}

func (m *mockAsaasQueueSub) Subscribe(topic string, handler func([]byte, messaging.MessageAck)) error {
	if m.subscribeErr != nil {
		return m.subscribeErr
	}
	m.subscribedTopic = topic
	m.handler = handler
	return nil
}

func (m *mockAsaasQueueSub) DeleteQueue(string) error           { return nil }
func (m *mockAsaasQueueSub) ValidateConnection() error          { return nil }
func (m *mockAsaasQueueSub) GetQueueLength(string) (int, error) { return 0, nil }

type mockAsaasAck struct {
	mu       sync.Mutex
	acked    bool
	nacked   bool
	requeued bool
}

func (m *mockAsaasAck) Ack() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.acked = true
	return nil
}

func (m *mockAsaasAck) Nack(requeue bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nacked = true
	m.requeued = requeue
	return nil
}

func (m *mockAsaasAck) DeliveryCount() int { return 1 }

func (m *mockAsaasAck) wasAcked() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.acked
}

func (m *mockAsaasAck) wasNacked() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.nacked
}

type mockAsaasSharedState struct {
	mu       sync.Mutex
	store    map[string]bool
	setNXErr error
}

func newMockAsaasSharedState() *mockAsaasSharedState {
	return &mockAsaasSharedState{store: make(map[string]bool)}
}

func (m *mockAsaasSharedState) SetNX(key, value string, ttl time.Duration) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.setNXErr != nil {
		return false, m.setNXErr
	}
	if m.store[key] {
		return false, nil
	}
	m.store[key] = true
	return true, nil
}

func (m *mockAsaasSharedState) SetString(string, string, time.Duration) error    { return nil }
func (m *mockAsaasSharedState) GetString(string) (string, error)                 { return "", nil }
func (m *mockAsaasSharedState) Del(...string) error                              { return nil }
func (m *mockAsaasSharedState) Exists(string) (bool, error)                      { return false, nil }
func (m *mockAsaasSharedState) Incr(string) (int64, error)                       { return 0, nil }
func (m *mockAsaasSharedState) Decr(string) (int64, error)                       { return 0, nil }
func (m *mockAsaasSharedState) TryIncr(string, int64) (bool, error)              { return false, nil }
func (m *mockAsaasSharedState) SAdd(string, ...string) error                     { return nil }
func (m *mockAsaasSharedState) SRem(string, ...string) error                     { return nil }
func (m *mockAsaasSharedState) SMembers(string) ([]string, error)                { return nil, nil }
func (m *mockAsaasSharedState) Publish(string, []byte) error                     { return nil }
func (m *mockAsaasSharedState) Subscribe(context.Context, string, func([]byte))  {}
func (m *mockAsaasSharedState) HSet(string, string, string) error                { return nil }
func (m *mockAsaasSharedState) HDel(string, string) error                        { return nil }
func (m *mockAsaasSharedState) HGetAll(string) (map[string]string, error)        { return nil, nil }
func (m *mockAsaasSharedState) IncrWithTTL(string, time.Duration) (int64, error) { return 1, nil }
func (m *mockAsaasSharedState) HIncrBy(string, string, int64) (int64, error)     { return 0, nil }
func (m *mockAsaasSharedState) Expire(string, time.Duration) (bool, error)       { return true, nil }
func (m *mockAsaasSharedState) IncrBy(string, int64) (int64, error)              { return 0, nil }
func (m *mockAsaasSharedState) DecrBy(string, int64) (int64, error)              { return 0, nil }
func (m *mockAsaasSharedState) TryIncrBy(string, int64, int64) (bool, error)     { return false, nil }

type mockAsaasHandler struct {
	mu      sync.Mutex
	calls   []*payment.AsaasWebhookEvent
	execErr error
}

func (m *mockAsaasHandler) Execute(event *payment.AsaasWebhookEvent) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, event)
	return m.execErr
}

func (m *mockAsaasHandler) callCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.calls)
}

func (m *mockAsaasHandler) lastCall() *payment.AsaasWebhookEvent {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.calls) == 0 {
		return nil
	}
	return m.calls[len(m.calls)-1]
}

func makeAsaasPayload(eventType, paymentID string, value float64) []byte {
	event := payment.AsaasWebhookEvent{
		ID:    "evt-" + paymentID,
		Event: eventType,
		Payment: payment.AsaasWebhookPayment{
			ID:    paymentID,
			Value: value,
		},
	}
	b, _ := json.Marshal(event)
	return b
}

func TestConsumeAsaas_SubscribesToCorrectTopic(t *testing.T) {
	sub := &mockAsaasQueueSub{}
	handler := &mockAsaasHandler{}
	state := newMockAsaasSharedState()

	uc := NewConsumeAsaasWebhookUseCase(sub, handler, state)
	if err := uc.Start(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if sub.subscribedTopic != webhook.TopicAsaasPayment {
		t.Fatalf("expected topic %s, got %s", webhook.TopicAsaasPayment, sub.subscribedTopic)
	}
}

func TestConsumeAsaas_SubscribeError(t *testing.T) {
	sub := &mockAsaasQueueSub{subscribeErr: errors.New("conn refused")}
	handler := &mockAsaasHandler{}
	state := newMockAsaasSharedState()

	uc := NewConsumeAsaasWebhookUseCase(sub, handler, state)
	if err := uc.Start(); err == nil {
		t.Fatal("expected error from subscribe failure")
	}
}

func TestConsumeAsaas_ProcessesPaymentEvent(t *testing.T) {
	sub := &mockAsaasQueueSub{}
	handler := &mockAsaasHandler{}
	state := newMockAsaasSharedState()

	uc := NewConsumeAsaasWebhookUseCase(sub, handler, state)
	_ = uc.Start()

	ack := &mockAsaasAck{}
	sub.handler(makeAsaasPayload("PAYMENT_RECEIVED", "pay_abc", 100.50), ack)

	time.Sleep(100 * time.Millisecond)

	if handler.callCount() != 1 {
		t.Fatalf("expected 1 handler call, got %d", handler.callCount())
	}
	last := handler.lastCall()
	if last.Event != "PAYMENT_RECEIVED" {
		t.Fatalf("expected event PAYMENT_RECEIVED, got %s", last.Event)
	}
	if last.Payment.ID != "pay_abc" {
		t.Fatalf("expected payment ID pay_abc, got %s", last.Payment.ID)
	}
	if !ack.wasAcked() {
		t.Fatal("expected ack")
	}
}

func TestConsumeAsaas_DuplicateEventIgnored(t *testing.T) {
	sub := &mockAsaasQueueSub{}
	handler := &mockAsaasHandler{}
	state := newMockAsaasSharedState()

	uc := NewConsumeAsaasWebhookUseCase(sub, handler, state)
	_ = uc.Start()

	payload := makeAsaasPayload("PAYMENT_RECEIVED", "pay_dup", 50)

	ack1 := &mockAsaasAck{}
	sub.handler(payload, ack1)
	time.Sleep(100 * time.Millisecond)

	ack2 := &mockAsaasAck{}
	sub.handler(payload, ack2)
	time.Sleep(100 * time.Millisecond)

	if handler.callCount() != 1 {
		t.Fatalf("duplicate should be ignored, got %d calls", handler.callCount())
	}
	if !ack2.wasAcked() {
		t.Fatal("duplicate should be acked, not nacked")
	}
}

func TestConsumeAsaas_DifferentEventsForSamePaymentNotDuplicate(t *testing.T) {
	sub := &mockAsaasQueueSub{}
	handler := &mockAsaasHandler{}
	state := newMockAsaasSharedState()

	uc := NewConsumeAsaasWebhookUseCase(sub, handler, state)
	_ = uc.Start()

	events := []string{"PAYMENT_CREATED", "PAYMENT_RECEIVED", "PAYMENT_CONFIRMED"}
	for _, evt := range events {
		ack := &mockAsaasAck{}
		sub.handler(makeAsaasPayload(evt, "pay_same", 100), ack)
		time.Sleep(50 * time.Millisecond)
	}

	time.Sleep(100 * time.Millisecond)

	if handler.callCount() != 3 {
		t.Fatalf("expected 3 calls for different events on same payment, got %d", handler.callCount())
	}
}

func TestConsumeAsaas_InvalidJSONNacks(t *testing.T) {
	sub := &mockAsaasQueueSub{}
	handler := &mockAsaasHandler{}
	state := newMockAsaasSharedState()

	uc := NewConsumeAsaasWebhookUseCase(sub, handler, state)
	_ = uc.Start()

	ack := &mockAsaasAck{}
	sub.handler([]byte("not json"), ack)

	time.Sleep(50 * time.Millisecond)

	if handler.callCount() != 0 {
		t.Fatal("handler should not be called for invalid JSON")
	}
	if !ack.wasNacked() {
		t.Fatal("invalid JSON should be nacked")
	}
}

func TestConsumeAsaas_HandlerErrorNacksWithRequeue(t *testing.T) {
	sub := &mockAsaasQueueSub{}
	handler := &mockAsaasHandler{execErr: errors.New("db error")}
	state := newMockAsaasSharedState()

	uc := NewConsumeAsaasWebhookUseCase(sub, handler, state)
	_ = uc.Start()

	ack := &mockAsaasAck{}
	sub.handler(makeAsaasPayload("PAYMENT_RECEIVED", "pay_err", 100), ack)

	time.Sleep(100 * time.Millisecond)

	if !ack.wasNacked() {
		t.Fatal("should nack with requeue when handler returns error")
	}
	if !ack.requeued {
		t.Fatal("should requeue on handler error for retry")
	}
}

type panicAsaasHandler struct{}

func (h *panicAsaasHandler) Execute(event *payment.AsaasWebhookEvent) error {
	panic("handler exploded")
}

func TestConsumeAsaas_PanicRecovery(t *testing.T) {
	sub := &mockAsaasQueueSub{}
	handler := &panicAsaasHandler{}
	state := newMockAsaasSharedState()

	uc := NewConsumeAsaasWebhookUseCase(sub, handler, state)
	_ = uc.Start()

	ack := &mockAsaasAck{}
	sub.handler(makeAsaasPayload("PAYMENT_RECEIVED", "pay_panic", 100), ack)

	time.Sleep(200 * time.Millisecond)

	if !ack.wasNacked() {
		t.Fatal("should nack after panic")
	}
	if ack.requeued {
		t.Fatal("should not requeue after panic")
	}
}

type errMockAsaasAck struct {
	mockAsaasAck
}

func (m *errMockAsaasAck) Ack() error {
	return errors.New("ack fail")
}

func TestConsumeAsaas_AckError(t *testing.T) {
	sub := &mockAsaasQueueSub{}
	handler := &mockAsaasHandler{}
	state := newMockAsaasSharedState()

	uc := NewConsumeAsaasWebhookUseCase(sub, handler, state)
	_ = uc.Start()

	ack := &errMockAsaasAck{}
	sub.handler(makeAsaasPayload("PAYMENT_RECEIVED", "pay_ack_err", 100), ack)

	time.Sleep(100 * time.Millisecond)

	if handler.callCount() != 1 {
		t.Fatalf("expected handler to be called, got %d", handler.callCount())
	}
}
