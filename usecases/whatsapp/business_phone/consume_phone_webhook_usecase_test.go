package businessphone_usecase

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"vozko/domain/messaging"
	"vozko/domain/webhook"
	businessphone "vozko/domain/whatsapp/business_phone"
)

type mockPhoneQueueSub struct {
	subscribedTopic string
	handler         func([]byte, messaging.MessageAck)
	subscribeErr    error
}

func (m *mockPhoneQueueSub) Subscribe(topic string, handler func([]byte, messaging.MessageAck)) error {
	if m.subscribeErr != nil {
		return m.subscribeErr
	}
	m.subscribedTopic = topic
	m.handler = handler
	return nil
}

func (m *mockPhoneQueueSub) DeleteQueue(string) error           { return nil }
func (m *mockPhoneQueueSub) ValidateConnection() error          { return nil }
func (m *mockPhoneQueueSub) GetQueueLength(string) (int, error) { return 0, nil }

type mockPhoneAck struct {
	mu       sync.Mutex
	acked    bool
	nacked   bool
	requeued bool
}

func (m *mockPhoneAck) Ack() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.acked = true
	return nil
}

func (m *mockPhoneAck) Nack(requeue bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nacked = true
	m.requeued = requeue
	return nil
}

func (m *mockPhoneAck) DeliveryCount() int { return 1 }

func (m *mockPhoneAck) wasAcked() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.acked
}

func (m *mockPhoneAck) wasNacked() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.nacked
}

type mockPhoneSharedState struct {
	mu       sync.Mutex
	store    map[string]bool
	setNXErr error
}

func newMockPhoneSharedState() *mockPhoneSharedState {
	return &mockPhoneSharedState{store: make(map[string]bool)}
}

func (m *mockPhoneSharedState) SetNX(key, value string, ttl time.Duration) (bool, error) {
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

func (m *mockPhoneSharedState) SetString(string, string, time.Duration) error    { return nil }
func (m *mockPhoneSharedState) GetString(string) (string, error)                 { return "", nil }
func (m *mockPhoneSharedState) Del(...string) error                              { return nil }
func (m *mockPhoneSharedState) Exists(string) (bool, error)                      { return false, nil }
func (m *mockPhoneSharedState) Incr(string) (int64, error)                       { return 0, nil }
func (m *mockPhoneSharedState) Decr(string) (int64, error)                       { return 0, nil }
func (m *mockPhoneSharedState) TryIncr(string, int64) (bool, error)              { return false, nil }
func (m *mockPhoneSharedState) SAdd(string, ...string) error                     { return nil }
func (m *mockPhoneSharedState) SRem(string, ...string) error                     { return nil }
func (m *mockPhoneSharedState) SMembers(string) ([]string, error)                { return nil, nil }
func (m *mockPhoneSharedState) Publish(string, []byte) error                     { return nil }
func (m *mockPhoneSharedState) Subscribe(context.Context, string, func([]byte))  {}
func (m *mockPhoneSharedState) HSet(string, string, string) error                { return nil }
func (m *mockPhoneSharedState) HDel(string, string) error                        { return nil }
func (m *mockPhoneSharedState) HGetAll(string) (map[string]string, error)        { return nil, nil }
func (m *mockPhoneSharedState) IncrWithTTL(string, time.Duration) (int64, error) { return 1, nil }
func (m *mockPhoneSharedState) HIncrBy(string, string, int64) (int64, error)     { return 0, nil }
func (m *mockPhoneSharedState) Expire(string, time.Duration) (bool, error)       { return true, nil }
func (m *mockPhoneSharedState) IncrBy(string, int64) (int64, error)              { return 0, nil }
func (m *mockPhoneSharedState) DecrBy(string, int64) (int64, error)              { return 0, nil }
func (m *mockPhoneSharedState) TryIncrBy(string, int64, int64) (bool, error)     { return false, nil }

type mockPhoneHandler struct {
	mu      sync.Mutex
	calls   []*businessphone.PhoneWebhookPayload
	execErr error
}

func (m *mockPhoneHandler) Execute(payload *businessphone.PhoneWebhookPayload) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, payload)
	return m.execErr
}

func (m *mockPhoneHandler) callCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.calls)
}

func makePhoneWebhookPayload(wabaID, field, displayPhone string) []byte {
	payload := businessphone.PhoneWebhookPayload{
		Object: "whatsapp_business_account",
		Entry: []businessphone.PhoneWebhookEntry{
			{
				ID: wabaID,
				Changes: []businessphone.PhoneWebhookChange{
					{
						Field: field,
						Value: businessphone.PhoneWebhookValue{
							DisplayPhoneNumber: displayPhone,
						},
					},
				},
			},
		},
	}
	b, _ := json.Marshal(payload)
	return b
}

func TestConsumePhone_SubscribesToCorrectTopic(t *testing.T) {
	sub := &mockPhoneQueueSub{}
	handler := &mockPhoneHandler{}
	state := newMockPhoneSharedState()

	uc := NewConsumePhoneWebhookUseCase(sub, handler, state)
	if err := uc.Start(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if sub.subscribedTopic != webhook.TopicWhatsAppPhone {
		t.Fatalf("expected topic %s, got %s", webhook.TopicWhatsAppPhone, sub.subscribedTopic)
	}
}

func TestConsumePhone_SubscribeError(t *testing.T) {
	sub := &mockPhoneQueueSub{subscribeErr: errors.New("conn refused")}
	handler := &mockPhoneHandler{}
	state := newMockPhoneSharedState()

	uc := NewConsumePhoneWebhookUseCase(sub, handler, state)
	if err := uc.Start(); err == nil {
		t.Fatal("expected error from subscribe failure")
	}
}

func TestConsumePhone_ProcessesQualityUpdate(t *testing.T) {
	sub := &mockPhoneQueueSub{}
	handler := &mockPhoneHandler{}
	state := newMockPhoneSharedState()

	uc := NewConsumePhoneWebhookUseCase(sub, handler, state)
	_ = uc.Start()

	ack := &mockPhoneAck{}
	sub.handler(makePhoneWebhookPayload("waba-1", businessphone.FieldPhoneNumberQualityUpdate, "+15551234567"), ack)

	time.Sleep(100 * time.Millisecond)

	if handler.callCount() != 1 {
		t.Fatalf("expected 1 handler call, got %d", handler.callCount())
	}
	if !ack.wasAcked() {
		t.Fatal("expected ack")
	}
}

func TestConsumePhone_ProcessesAllPhoneFields(t *testing.T) {
	fields := []string{
		businessphone.FieldPhoneNumberQualityUpdate,
		businessphone.FieldPhoneNumberNameUpdate,
		businessphone.FieldAccountAlerts,
		businessphone.FieldBusinessCapabilityUpdate,
		businessphone.FieldAccountUpdate,
		businessphone.FieldAccountReviewUpdate,
		businessphone.FieldBusinessStatusUpdate,
	}

	for _, field := range fields {
		sub := &mockPhoneQueueSub{}
		handler := &mockPhoneHandler{}
		state := newMockPhoneSharedState()

		uc := NewConsumePhoneWebhookUseCase(sub, handler, state)
		_ = uc.Start()

		ack := &mockPhoneAck{}
		sub.handler(makePhoneWebhookPayload("waba-1", field, "+15551234567"), ack)

		time.Sleep(100 * time.Millisecond)

		if handler.callCount() != 1 {
			t.Fatalf("field %s: expected 1 handler call, got %d", field, handler.callCount())
		}
	}
}

func TestConsumePhone_DuplicateEventIgnored(t *testing.T) {
	sub := &mockPhoneQueueSub{}
	handler := &mockPhoneHandler{}
	state := newMockPhoneSharedState()

	uc := NewConsumePhoneWebhookUseCase(sub, handler, state)
	_ = uc.Start()

	payload := makePhoneWebhookPayload("waba-1", businessphone.FieldPhoneNumberQualityUpdate, "+15551234567")

	ack1 := &mockPhoneAck{}
	sub.handler(payload, ack1)
	time.Sleep(100 * time.Millisecond)

	ack2 := &mockPhoneAck{}
	sub.handler(payload, ack2)
	time.Sleep(100 * time.Millisecond)

	if handler.callCount() != 1 {
		t.Fatalf("duplicate should be ignored, got %d calls", handler.callCount())
	}
	if !ack2.wasAcked() {
		t.Fatal("duplicate should be acked")
	}
}

func TestConsumePhone_DifferentFieldsSameWABANotDuplicate(t *testing.T) {
	sub := &mockPhoneQueueSub{}
	handler := &mockPhoneHandler{}
	state := newMockPhoneSharedState()

	uc := NewConsumePhoneWebhookUseCase(sub, handler, state)
	_ = uc.Start()

	fields := []string{
		businessphone.FieldPhoneNumberQualityUpdate,
		businessphone.FieldPhoneNumberNameUpdate,
	}

	for _, field := range fields {
		ack := &mockPhoneAck{}
		sub.handler(makePhoneWebhookPayload("waba-1", field, "+15551234567"), ack)
		time.Sleep(50 * time.Millisecond)
	}

	time.Sleep(100 * time.Millisecond)

	if handler.callCount() != 2 {
		t.Fatalf("different fields should not be deduplicated, got %d calls", handler.callCount())
	}
}

func TestConsumePhone_DifferentPhonesSameFieldNotDuplicate(t *testing.T) {
	sub := &mockPhoneQueueSub{}
	handler := &mockPhoneHandler{}
	state := newMockPhoneSharedState()

	uc := NewConsumePhoneWebhookUseCase(sub, handler, state)
	_ = uc.Start()

	phones := []string{"+15551111111", "+15552222222"}
	for _, phone := range phones {
		ack := &mockPhoneAck{}
		sub.handler(makePhoneWebhookPayload("waba-1", businessphone.FieldPhoneNumberQualityUpdate, phone), ack)
		time.Sleep(50 * time.Millisecond)
	}

	time.Sleep(100 * time.Millisecond)

	if handler.callCount() != 2 {
		t.Fatalf("different phones should not be deduplicated, got %d calls", handler.callCount())
	}
}

func TestConsumePhone_InvalidJSONNacks(t *testing.T) {
	sub := &mockPhoneQueueSub{}
	handler := &mockPhoneHandler{}
	state := newMockPhoneSharedState()

	uc := NewConsumePhoneWebhookUseCase(sub, handler, state)
	_ = uc.Start()

	ack := &mockPhoneAck{}
	sub.handler([]byte("bad json"), ack)

	time.Sleep(50 * time.Millisecond)

	if handler.callCount() != 0 {
		t.Fatal("handler should not be called for invalid JSON")
	}
	if !ack.wasNacked() {
		t.Fatal("invalid JSON should be nacked")
	}
}

func TestConsumePhone_HandlerErrorStillAcks(t *testing.T) {
	sub := &mockPhoneQueueSub{}
	handler := &mockPhoneHandler{execErr: errors.New("repo error")}
	state := newMockPhoneSharedState()

	uc := NewConsumePhoneWebhookUseCase(sub, handler, state)
	_ = uc.Start()

	ack := &mockPhoneAck{}
	sub.handler(makePhoneWebhookPayload("waba-1", businessphone.FieldPhoneNumberQualityUpdate, "+15551234567"), ack)

	time.Sleep(100 * time.Millisecond)

	if !ack.wasAcked() {
		t.Fatal("should ack even when handler returns error")
	}
}

func TestExtractPhoneDedupKey_Standard(t *testing.T) {
	payload := &businessphone.PhoneWebhookPayload{
		Entry: []businessphone.PhoneWebhookEntry{
			{
				ID: "waba-123",
				Changes: []businessphone.PhoneWebhookChange{
					{
						Field: businessphone.FieldPhoneNumberQualityUpdate,
						Value: businessphone.PhoneWebhookValue{
							DisplayPhoneNumber: "+15551234567",
						},
					},
				},
			},
		},
	}
	key := extractPhoneDedupKey(payload)
	expected := "waba-123:phone_number_quality_update:+15551234567"
	if key != expected {
		t.Fatalf("expected '%s', got '%s'", expected, key)
	}
}

func TestExtractPhoneDedupKey_NilPayload(t *testing.T) {
	key := extractPhoneDedupKey(nil)
	if key != "" {
		t.Fatalf("expected empty for nil payload, got '%s'", key)
	}
}

func TestExtractPhoneDedupKey_EmptyPayload(t *testing.T) {
	payload := &businessphone.PhoneWebhookPayload{}
	key := extractPhoneDedupKey(payload)
	if key != "" {
		t.Fatalf("expected empty for empty payload, got '%s'", key)
	}
}
