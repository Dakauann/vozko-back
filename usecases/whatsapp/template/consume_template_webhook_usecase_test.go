package template_usecase

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"vozko/domain/messaging"
	"vozko/domain/webhook"
	whatsapptemplate "vozko/domain/whatsapp/template"
)

type mockTemplateQueueSub struct {
	subscribedTopic string
	handler         func([]byte, messaging.MessageAck)
	subscribeErr    error
}

func (m *mockTemplateQueueSub) Subscribe(topic string, handler func([]byte, messaging.MessageAck)) error {
	if m.subscribeErr != nil {
		return m.subscribeErr
	}
	m.subscribedTopic = topic
	m.handler = handler
	return nil
}

func (m *mockTemplateQueueSub) DeleteQueue(string) error           { return nil }
func (m *mockTemplateQueueSub) ValidateConnection() error          { return nil }
func (m *mockTemplateQueueSub) GetQueueLength(string) (int, error) { return 0, nil }

type mockTemplateAck struct {
	mu       sync.Mutex
	acked    bool
	nacked   bool
	requeued bool
}

func (m *mockTemplateAck) Ack() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.acked = true
	return nil
}

func (m *mockTemplateAck) Nack(requeue bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nacked = true
	m.requeued = requeue
	return nil
}

func (m *mockTemplateAck) DeliveryCount() int { return 1 }

func (m *mockTemplateAck) wasAcked() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.acked
}

func (m *mockTemplateAck) wasNacked() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.nacked
}

type mockTemplateSharedState struct {
	mu       sync.Mutex
	store    map[string]bool
	setNXErr error
}

func newMockTemplateSharedState() *mockTemplateSharedState {
	return &mockTemplateSharedState{store: make(map[string]bool)}
}

func (m *mockTemplateSharedState) SetNX(key, value string, ttl time.Duration) (bool, error) {
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

func (m *mockTemplateSharedState) SetString(string, string, time.Duration) error    { return nil }
func (m *mockTemplateSharedState) GetString(string) (string, error)                 { return "", nil }
func (m *mockTemplateSharedState) Del(...string) error                              { return nil }
func (m *mockTemplateSharedState) Exists(string) (bool, error)                      { return false, nil }
func (m *mockTemplateSharedState) Incr(string) (int64, error)                       { return 0, nil }
func (m *mockTemplateSharedState) Decr(string) (int64, error)                       { return 0, nil }
func (m *mockTemplateSharedState) TryIncr(string, int64) (bool, error)              { return false, nil }
func (m *mockTemplateSharedState) SAdd(string, ...string) error                     { return nil }
func (m *mockTemplateSharedState) SRem(string, ...string) error                     { return nil }
func (m *mockTemplateSharedState) SMembers(string) ([]string, error)                { return nil, nil }
func (m *mockTemplateSharedState) Publish(string, []byte) error                     { return nil }
func (m *mockTemplateSharedState) Subscribe(context.Context, string, func([]byte))  {}
func (m *mockTemplateSharedState) HSet(string, string, string) error                { return nil }
func (m *mockTemplateSharedState) HDel(string, string) error                        { return nil }
func (m *mockTemplateSharedState) HGetAll(string) (map[string]string, error)        { return nil, nil }
func (m *mockTemplateSharedState) IncrWithTTL(string, time.Duration) (int64, error) { return 1, nil }
func (m *mockTemplateSharedState) HIncrBy(string, string, int64) (int64, error)     { return 0, nil }
func (m *mockTemplateSharedState) Expire(string, time.Duration) (bool, error)       { return true, nil }
func (m *mockTemplateSharedState) IncrBy(string, int64) (int64, error)              { return 0, nil }
func (m *mockTemplateSharedState) DecrBy(string, int64) (int64, error)              { return 0, nil }
func (m *mockTemplateSharedState) TryIncrBy(string, int64, int64) (bool, error)     { return false, nil }

type mockTemplateHandler struct {
	mu      sync.Mutex
	calls   []*whatsapptemplate.TemplateWebhookPayload
	execErr error
}

func (m *mockTemplateHandler) Execute(payload *whatsapptemplate.TemplateWebhookPayload) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, payload)
	return m.execErr
}

func (m *mockTemplateHandler) callCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.calls)
}

func makeTemplateWebhookPayload(wabaID, field, event, name, language string, templateID int64) []byte {
	payload := whatsapptemplate.TemplateWebhookPayload{
		Object: "whatsapp_business_account",
		Entry: []whatsapptemplate.TemplateWebhookEntry{
			{
				ID:   wabaID,
				Time: time.Now().Unix(),
				Changes: []whatsapptemplate.TemplateWebhookChange{
					{
						Field: field,
						Value: whatsapptemplate.TemplateWebhookValue{
							Event:                   event,
							MessageTemplateID:       templateID,
							MessageTemplateName:     name,
							MessageTemplateLanguage: language,
						},
					},
				},
			},
		},
	}
	b, _ := json.Marshal(payload)
	return b
}

func TestConsumeTemplate_SubscribesToCorrectTopic(t *testing.T) {
	sub := &mockTemplateQueueSub{}
	handler := &mockTemplateHandler{}
	state := newMockTemplateSharedState()

	uc := NewConsumeTemplateWebhookUseCase(sub, handler, state)
	if err := uc.Start(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if sub.subscribedTopic != webhook.TopicWhatsAppTemplate {
		t.Fatalf("expected topic %s, got %s", webhook.TopicWhatsAppTemplate, sub.subscribedTopic)
	}
}

func TestConsumeTemplate_SubscribeError(t *testing.T) {
	sub := &mockTemplateQueueSub{subscribeErr: errors.New("conn refused")}
	handler := &mockTemplateHandler{}
	state := newMockTemplateSharedState()

	uc := NewConsumeTemplateWebhookUseCase(sub, handler, state)
	if err := uc.Start(); err == nil {
		t.Fatal("expected error from subscribe failure")
	}
}

func TestConsumeTemplate_ProcessesStatusUpdate(t *testing.T) {
	sub := &mockTemplateQueueSub{}
	handler := &mockTemplateHandler{}
	state := newMockTemplateSharedState()

	uc := NewConsumeTemplateWebhookUseCase(sub, handler, state)
	_ = uc.Start()

	ack := &mockTemplateAck{}
	sub.handler(makeTemplateWebhookPayload(
		"waba-1",
		whatsapptemplate.FieldMessageTemplateStatusUpdate,
		"APPROVED",
		"my_template",
		"pt_BR",
		12345,
	), ack)

	time.Sleep(100 * time.Millisecond)

	if handler.callCount() != 1 {
		t.Fatalf("expected 1 handler call, got %d", handler.callCount())
	}
	if !ack.wasAcked() {
		t.Fatal("expected ack")
	}
}

func TestConsumeTemplate_ProcessesAllTemplateFields(t *testing.T) {
	fields := []string{
		whatsapptemplate.FieldMessageTemplateStatusUpdate,
		whatsapptemplate.FieldMessageTemplateQualityUpdate,
		whatsapptemplate.FieldMessageTemplateComponentsUpdate,
		whatsapptemplate.FieldTemplateCategoryUpdate,
	}

	for _, field := range fields {
		sub := &mockTemplateQueueSub{}
		handler := &mockTemplateHandler{}
		state := newMockTemplateSharedState()

		uc := NewConsumeTemplateWebhookUseCase(sub, handler, state)
		_ = uc.Start()

		ack := &mockTemplateAck{}
		sub.handler(makeTemplateWebhookPayload("waba-1", field, "APPROVED", "tpl", "en", 999), ack)

		time.Sleep(100 * time.Millisecond)

		if handler.callCount() != 1 {
			t.Fatalf("field %s: expected 1 handler call, got %d", field, handler.callCount())
		}
	}
}

func TestConsumeTemplate_DuplicateEventIgnored(t *testing.T) {
	sub := &mockTemplateQueueSub{}
	handler := &mockTemplateHandler{}
	state := newMockTemplateSharedState()

	uc := NewConsumeTemplateWebhookUseCase(sub, handler, state)
	_ = uc.Start()

	payload := makeTemplateWebhookPayload(
		"waba-1",
		whatsapptemplate.FieldMessageTemplateStatusUpdate,
		"APPROVED",
		"tpl",
		"pt_BR",
		12345,
	)

	ack1 := &mockTemplateAck{}
	sub.handler(payload, ack1)
	time.Sleep(100 * time.Millisecond)

	ack2 := &mockTemplateAck{}
	sub.handler(payload, ack2)
	time.Sleep(100 * time.Millisecond)

	if handler.callCount() != 1 {
		t.Fatalf("duplicate should be ignored, got %d calls", handler.callCount())
	}
	if !ack2.wasAcked() {
		t.Fatal("duplicate should be acked")
	}
}

func TestConsumeTemplate_DifferentEventsForSameTemplateNotDuplicate(t *testing.T) {
	sub := &mockTemplateQueueSub{}
	handler := &mockTemplateHandler{}
	state := newMockTemplateSharedState()

	uc := NewConsumeTemplateWebhookUseCase(sub, handler, state)
	_ = uc.Start()

	events := []string{"APPROVED", "REJECTED", "PAUSED"}
	for _, evt := range events {
		ack := &mockTemplateAck{}
		sub.handler(makeTemplateWebhookPayload(
			"waba-1",
			whatsapptemplate.FieldMessageTemplateStatusUpdate,
			evt,
			"tpl",
			"pt_BR",
			12345,
		), ack)
		time.Sleep(50 * time.Millisecond)
	}

	time.Sleep(100 * time.Millisecond)

	if handler.callCount() != 3 {
		t.Fatalf("different events should not be deduplicated, got %d calls", handler.callCount())
	}
}

func TestConsumeTemplate_DifferentLanguagesNotDuplicate(t *testing.T) {
	sub := &mockTemplateQueueSub{}
	handler := &mockTemplateHandler{}
	state := newMockTemplateSharedState()

	uc := NewConsumeTemplateWebhookUseCase(sub, handler, state)
	_ = uc.Start()

	langs := []string{"pt_BR", "en_US", "es"}
	for _, lang := range langs {
		ack := &mockTemplateAck{}
		sub.handler(makeTemplateWebhookPayload(
			"waba-1",
			whatsapptemplate.FieldMessageTemplateStatusUpdate,
			"APPROVED",
			"tpl",
			lang,
			12345,
		), ack)
		time.Sleep(50 * time.Millisecond)
	}

	time.Sleep(100 * time.Millisecond)

	if handler.callCount() != 3 {
		t.Fatalf("different languages should not be deduplicated, got %d calls", handler.callCount())
	}
}

func TestConsumeTemplate_DifferentTemplateIDsNotDuplicate(t *testing.T) {
	sub := &mockTemplateQueueSub{}
	handler := &mockTemplateHandler{}
	state := newMockTemplateSharedState()

	uc := NewConsumeTemplateWebhookUseCase(sub, handler, state)
	_ = uc.Start()

	ids := []int64{111, 222, 333}
	for _, id := range ids {
		ack := &mockTemplateAck{}
		sub.handler(makeTemplateWebhookPayload(
			"waba-1",
			whatsapptemplate.FieldMessageTemplateStatusUpdate,
			"APPROVED",
			"tpl",
			"pt_BR",
			id,
		), ack)
		time.Sleep(50 * time.Millisecond)
	}

	time.Sleep(100 * time.Millisecond)

	if handler.callCount() != 3 {
		t.Fatalf("different template IDs should not be deduplicated, got %d calls", handler.callCount())
	}
}

func TestConsumeTemplate_InvalidJSONNacks(t *testing.T) {
	sub := &mockTemplateQueueSub{}
	handler := &mockTemplateHandler{}
	state := newMockTemplateSharedState()

	uc := NewConsumeTemplateWebhookUseCase(sub, handler, state)
	_ = uc.Start()

	ack := &mockTemplateAck{}
	sub.handler([]byte("bad json"), ack)

	time.Sleep(50 * time.Millisecond)

	if handler.callCount() != 0 {
		t.Fatal("handler should not be called for invalid JSON")
	}
	if !ack.wasNacked() {
		t.Fatal("invalid JSON should be nacked")
	}
}

func TestConsumeTemplate_HandlerErrorRequeues(t *testing.T) {
	sub := &mockTemplateQueueSub{}
	handler := &mockTemplateHandler{execErr: errors.New("repo error")}
	state := newMockTemplateSharedState()

	uc := NewConsumeTemplateWebhookUseCase(sub, handler, state)
	_ = uc.Start()

	ack := &mockTemplateAck{}
	sub.handler(makeTemplateWebhookPayload(
		"waba-1",
		whatsapptemplate.FieldMessageTemplateStatusUpdate,
		"APPROVED",
		"tpl",
		"pt_BR",
		12345,
	), ack)

	time.Sleep(100 * time.Millisecond)

	if ack.wasAcked() {
		t.Fatal("should not ack when handler returns error")
	}
	if !ack.wasNacked() {
		t.Fatal("should nack when handler returns error")
	}
	if !ack.requeued {
		t.Fatal("handler error should requeue the webhook event")
	}
}

func TestExtractTemplateDedupKey_Standard(t *testing.T) {
	payload := &whatsapptemplate.TemplateWebhookPayload{
		Entry: []whatsapptemplate.TemplateWebhookEntry{
			{
				ID: "waba-1",
				Changes: []whatsapptemplate.TemplateWebhookChange{
					{
						Field: whatsapptemplate.FieldMessageTemplateStatusUpdate,
						Value: whatsapptemplate.TemplateWebhookValue{
							MessageTemplateID:       12345,
							Event:                   "APPROVED",
							MessageTemplateLanguage: "pt_BR",
						},
					},
				},
			},
		},
	}
	key := extractTemplateDedupKey(payload)
	expected := "message_template_status_update:12345:APPROVED:pt_BR"
	if key != expected {
		t.Fatalf("expected '%s', got '%s'", expected, key)
	}
}

func TestExtractTemplateDedupKey_NilPayload(t *testing.T) {
	key := extractTemplateDedupKey(nil)
	if key != "" {
		t.Fatalf("expected empty for nil payload, got '%s'", key)
	}
}

func TestExtractTemplateDedupKey_EmptyPayload(t *testing.T) {
	payload := &whatsapptemplate.TemplateWebhookPayload{}
	key := extractTemplateDedupKey(payload)
	if key != "" {
		t.Fatalf("expected empty for empty payload, got '%s'", key)
	}
}
