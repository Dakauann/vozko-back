package conversation_usecase

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"vozko/domain/conversation"
	"vozko/domain/messaging"
	"vozko/domain/webhook"
)

type mockWhatsAppQueueSub struct {
	subscribedTopic string
	handler         func([]byte, messaging.MessageAck)
	subscribeErr    error
}

type mockWhatsAppQueuePub struct {
	mu         sync.Mutex
	calls      []mockWhatsAppPublishCall
	publishErr error
}

type mockWhatsAppPublishCall struct {
	topic   string
	message []byte
	delay   time.Duration
}

func (m *mockWhatsAppQueueSub) Subscribe(topic string, handler func([]byte, messaging.MessageAck)) error {
	if m.subscribeErr != nil {
		return m.subscribeErr
	}
	m.subscribedTopic = topic
	m.handler = handler
	return nil
}

func (m *mockWhatsAppQueueSub) DeleteQueue(string) error           { return nil }
func (m *mockWhatsAppQueueSub) ValidateConnection() error          { return nil }
func (m *mockWhatsAppQueueSub) GetQueueLength(string) (int, error) { return 0, nil }

func (m *mockWhatsAppQueuePub) Publish(topic string, message []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, mockWhatsAppPublishCall{topic: topic, message: append([]byte(nil), message...)})
	return nil
}

func (m *mockWhatsAppQueuePub) PublishWithDelay(topic string, message []byte, delay time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.publishErr != nil {
		return m.publishErr
	}
	m.calls = append(m.calls, mockWhatsAppPublishCall{topic: topic, message: append([]byte(nil), message...), delay: delay})
	return nil
}

func (m *mockWhatsAppQueuePub) ValidateConnection() error { return nil }

func (m *mockWhatsAppQueuePub) callCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.calls)
}

func (m *mockWhatsAppQueuePub) firstCall() mockWhatsAppPublishCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.calls) == 0 {
		return mockWhatsAppPublishCall{}
	}
	return m.calls[0]
}

type mockMessageAck struct {
	mu            sync.Mutex
	acked         bool
	nacked        bool
	requeued      bool
	deliveryCount int
}

func (m *mockMessageAck) Ack() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.acked = true
	return nil
}

func (m *mockMessageAck) Nack(requeue bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nacked = true
	m.requeued = requeue
	return nil
}

func (m *mockMessageAck) DeliveryCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.deliveryCount < 1 {
		return 1
	}
	return m.deliveryCount
}

func (m *mockMessageAck) wasAcked() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.acked
}

func (m *mockMessageAck) wasNacked() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.nacked
}

func (m *mockMessageAck) wasRequeued() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.requeued
}

type mockWhatsAppSharedState struct {
	mu        sync.Mutex
	store     map[string]bool
	counters  map[string]int64
	setNXErr  error
	setStrErr error
	existsErr error
	delErr    error
	incrErr   error
}

func newMockWhatsAppSharedState() *mockWhatsAppSharedState {
	return &mockWhatsAppSharedState{
		store:    make(map[string]bool),
		counters: make(map[string]int64),
	}
}

func (m *mockWhatsAppSharedState) SetNX(key, value string, ttl time.Duration) (bool, error) {
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

func (m *mockWhatsAppSharedState) SetString(key, value string, ttl time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.setStrErr != nil {
		return m.setStrErr
	}
	m.store[key] = true
	return nil
}
func (m *mockWhatsAppSharedState) GetString(string) (string, error) { return "", nil }
func (m *mockWhatsAppSharedState) Del(keys ...string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.delErr != nil {
		return m.delErr
	}
	for _, key := range keys {
		delete(m.store, key)
		delete(m.counters, key)
	}
	return nil
}
func (m *mockWhatsAppSharedState) Exists(key string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.existsErr != nil {
		return false, m.existsErr
	}
	return m.store[key], nil
}
func (m *mockWhatsAppSharedState) Incr(string) (int64, error)                      { return 0, nil }
func (m *mockWhatsAppSharedState) Decr(string) (int64, error)                      { return 0, nil }
func (m *mockWhatsAppSharedState) TryIncr(string, int64) (bool, error)             { return false, nil }
func (m *mockWhatsAppSharedState) SAdd(string, ...string) error                    { return nil }
func (m *mockWhatsAppSharedState) SRem(string, ...string) error                    { return nil }
func (m *mockWhatsAppSharedState) SMembers(string) ([]string, error)               { return nil, nil }
func (m *mockWhatsAppSharedState) Publish(string, []byte) error                    { return nil }
func (m *mockWhatsAppSharedState) Subscribe(context.Context, string, func([]byte)) {}
func (m *mockWhatsAppSharedState) HSet(string, string, string) error               { return nil }
func (m *mockWhatsAppSharedState) HDel(string, string) error                       { return nil }
func (m *mockWhatsAppSharedState) HGetAll(string) (map[string]string, error)       { return nil, nil }
func (m *mockWhatsAppSharedState) IncrWithTTL(key string, _ time.Duration) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.incrErr != nil {
		return 0, m.incrErr
	}
	m.counters[key]++
	return m.counters[key], nil
}
func (m *mockWhatsAppSharedState) HIncrBy(string, string, int64) (int64, error) { return 0, nil }
func (m *mockWhatsAppSharedState) Expire(string, time.Duration) (bool, error)   { return true, nil }
func (m *mockWhatsAppSharedState) IncrBy(string, int64) (int64, error)          { return 0, nil }
func (m *mockWhatsAppSharedState) DecrBy(string, int64) (int64, error)          { return 0, nil }
func (m *mockWhatsAppSharedState) TryIncrBy(string, int64, int64) (bool, error) { return false, nil }

type mockWhatsAppHandler struct {
	mu       sync.Mutex
	calls    []*conversation.WhatsAppWebhookPayload
	execErr  error
	execErrs []error
}

func (m *mockWhatsAppHandler) Execute(ctx context.Context, payload *conversation.WhatsAppWebhookPayload) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, payload)
	if len(m.execErrs) > 0 {
		err := m.execErrs[0]
		m.execErrs = m.execErrs[1:]
		return err
	}
	return m.execErr
}

func (m *mockWhatsAppHandler) callCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.calls)
}

func makeTextMessagePayload(msgID, from, text string) []byte {
	payload := conversation.WhatsAppWebhookPayload{
		Object: "whatsapp_business_account",
		Entry: []conversation.WhatsAppEntry{
			{
				ID: "entry-1",
				Changes: []conversation.WhatsAppChange{
					{
						Field: "messages",
						Value: conversation.WhatsAppChangeValue{
							Messages: []conversation.WhatsAppMessage{
								{
									ID:   msgID,
									From: from,
									Type: "text",
									Text: &conversation.WhatsAppTextMessage{Body: text},
								},
							},
						},
					},
				},
			},
		},
	}
	b, _ := json.Marshal(payload)
	return b
}

func makeAudioMessagePayload(msgID, audioID, from string) []byte {
	payload := conversation.WhatsAppWebhookPayload{
		Object: "whatsapp_business_account",
		Entry: []conversation.WhatsAppEntry{
			{
				ID: "entry-1",
				Changes: []conversation.WhatsAppChange{
					{
						Field: "messages",
						Value: conversation.WhatsAppChangeValue{
							Messages: []conversation.WhatsAppMessage{
								{
									ID:   msgID,
									From: from,
									Type: "audio",
									Audio: &conversation.WhatsAppAudioMessage{
										ID:       audioID,
										MimeType: "audio/ogg",
										Voice:    true,
									},
								},
							},
						},
					},
				},
			},
		},
	}
	b, _ := json.Marshal(payload)
	return b
}

func makeStatusPayloadWA(wamid, status, recipientID string) []byte {
	payload := conversation.WhatsAppWebhookPayload{
		Object: "whatsapp_business_account",
		Entry: []conversation.WhatsAppEntry{
			{
				ID: "entry-1",
				Changes: []conversation.WhatsAppChange{
					{
						Field: "messages",
						Value: conversation.WhatsAppChangeValue{
							Statuses: []conversation.WhatsAppStatus{
								{
									ID:          wamid,
									Status:      status,
									RecipientID: recipientID,
									Timestamp:   "1700000000",
								},
							},
						},
					},
				},
			},
		},
	}
	b, _ := json.Marshal(payload)
	return b
}

func TestConsumeWhatsAppMessage_SubscribesToCorrectTopic(t *testing.T) {
	sub := &mockWhatsAppQueueSub{}
	handler := &mockWhatsAppHandler{}
	state := newMockWhatsAppSharedState()

	uc := NewConsumeWhatsAppMessageWebhookUseCase(sub, handler, state)
	if err := uc.Start(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if sub.subscribedTopic != webhook.TopicWhatsAppMessage {
		t.Fatalf("expected topic %s, got %s", webhook.TopicWhatsAppMessage, sub.subscribedTopic)
	}
}

func TestConsumeWhatsAppMessage_SubscribeError(t *testing.T) {
	sub := &mockWhatsAppQueueSub{subscribeErr: errors.New("connection refused")}
	handler := &mockWhatsAppHandler{}
	state := newMockWhatsAppSharedState()

	uc := NewConsumeWhatsAppMessageWebhookUseCase(sub, handler, state)
	if err := uc.Start(); err == nil {
		t.Fatal("expected error from subscribe failure")
	}
}

func TestConsumeWhatsAppMessage_ProcessesTextMessage(t *testing.T) {
	sub := &mockWhatsAppQueueSub{}
	handler := &mockWhatsAppHandler{}
	state := newMockWhatsAppSharedState()

	uc := NewConsumeWhatsAppMessageWebhookUseCase(sub, handler, state)
	_ = uc.Start()

	ack := &mockMessageAck{}
	sub.handler(makeTextMessagePayload("wamid.123", "5511999999999", "hello"), ack)

	time.Sleep(100 * time.Millisecond)

	if handler.callCount() != 1 {
		t.Fatalf("expected handler called once, got %d", handler.callCount())
	}
	if !ack.wasAcked() {
		t.Fatal("expected message to be acked")
	}
}

func TestConsumeWhatsAppMessage_ProcessesAudioMessage(t *testing.T) {
	sub := &mockWhatsAppQueueSub{}
	handler := &mockWhatsAppHandler{}
	state := newMockWhatsAppSharedState()

	uc := NewConsumeWhatsAppMessageWebhookUseCase(sub, handler, state)
	_ = uc.Start()

	ack := &mockMessageAck{}
	sub.handler(makeAudioMessagePayload("wamid.456", "audio-id-789", "5511999999999"), ack)

	time.Sleep(100 * time.Millisecond)

	if handler.callCount() != 1 {
		t.Fatalf("expected handler called once, got %d", handler.callCount())
	}
	if !ack.wasAcked() {
		t.Fatal("expected message to be acked")
	}
}

func TestConsumeWhatsAppMessage_ProcessesStatusEvent(t *testing.T) {
	sub := &mockWhatsAppQueueSub{}
	handler := &mockWhatsAppHandler{}
	state := newMockWhatsAppSharedState()

	uc := NewConsumeWhatsAppMessageWebhookUseCase(sub, handler, state)
	_ = uc.Start()

	ack := &mockMessageAck{}
	sub.handler(makeStatusPayloadWA("wamid.abc", "delivered", "5511999999999"), ack)

	time.Sleep(100 * time.Millisecond)

	if handler.callCount() != 1 {
		t.Fatalf("expected handler called once for status event, got %d", handler.callCount())
	}
	if !ack.wasAcked() {
		t.Fatal("expected status message to be acked")
	}
}

func TestConsumeWhatsAppMessage_DifferentStatusEventsNotDuplicated(t *testing.T) {
	sub := &mockWhatsAppQueueSub{}
	handler := &mockWhatsAppHandler{}
	state := newMockWhatsAppSharedState()

	uc := NewConsumeWhatsAppMessageWebhookUseCase(sub, handler, state)
	_ = uc.Start()

	statuses := []string{"sent", "delivered", "read"}
	for _, status := range statuses {
		ack := &mockMessageAck{}
		sub.handler(makeStatusPayloadWA("wamid.same-id", status, "5511999999999"), ack)
		time.Sleep(50 * time.Millisecond)
	}

	time.Sleep(100 * time.Millisecond)

	if handler.callCount() != 3 {
		t.Fatalf("expected 3 handler calls (sent, delivered, read), got %d", handler.callCount())
	}
}

func TestConsumeWhatsAppMessage_DuplicateTextMessageIgnored(t *testing.T) {
	sub := &mockWhatsAppQueueSub{}
	handler := &mockWhatsAppHandler{}
	state := newMockWhatsAppSharedState()

	uc := NewConsumeWhatsAppMessageWebhookUseCase(sub, handler, state)
	_ = uc.Start()

	payload := makeTextMessagePayload("wamid.dup", "5511999999999", "hello")

	ack1 := &mockMessageAck{}
	sub.handler(payload, ack1)
	time.Sleep(100 * time.Millisecond)

	ack2 := &mockMessageAck{}
	sub.handler(payload, ack2)
	time.Sleep(100 * time.Millisecond)

	if handler.callCount() != 1 {
		t.Fatalf("expected handler called once, duplicate should be ignored, got %d", handler.callCount())
	}
	if !ack1.wasAcked() {
		t.Fatal("first message should be acked")
	}
	if !ack2.wasAcked() {
		t.Fatal("duplicate should be acked (not nacked)")
	}
}

func TestConsumeWhatsAppMessage_DuplicateAudioMessageIgnored(t *testing.T) {
	sub := &mockWhatsAppQueueSub{}
	handler := &mockWhatsAppHandler{}
	state := newMockWhatsAppSharedState()

	uc := NewConsumeWhatsAppMessageWebhookUseCase(sub, handler, state)
	_ = uc.Start()

	payload := makeAudioMessagePayload("wamid.audio-dup", "audio-id-same", "5511999999999")

	ack1 := &mockMessageAck{}
	sub.handler(payload, ack1)
	time.Sleep(100 * time.Millisecond)

	ack2 := &mockMessageAck{}
	sub.handler(payload, ack2)
	time.Sleep(100 * time.Millisecond)

	if handler.callCount() != 1 {
		t.Fatalf("duplicate audio should be ignored, handler called %d times", handler.callCount())
	}
}

func TestConsumeWhatsAppMessage_DuplicateStatusEventIgnored(t *testing.T) {
	sub := &mockWhatsAppQueueSub{}
	handler := &mockWhatsAppHandler{}
	state := newMockWhatsAppSharedState()

	uc := NewConsumeWhatsAppMessageWebhookUseCase(sub, handler, state)
	_ = uc.Start()

	payload := makeStatusPayloadWA("wamid.status-dup", "delivered", "5511999999999")

	ack1 := &mockMessageAck{}
	sub.handler(payload, ack1)
	time.Sleep(100 * time.Millisecond)

	ack2 := &mockMessageAck{}
	sub.handler(payload, ack2)
	time.Sleep(100 * time.Millisecond)

	if handler.callCount() != 1 {
		t.Fatalf("duplicate status event should be ignored, handler called %d times", handler.callCount())
	}
}

func TestConsumeWhatsAppMessage_DuplicateFailedStatusEventNotDeduped(t *testing.T) {
	sub := &mockWhatsAppQueueSub{}
	handler := &mockWhatsAppHandler{execErr: conversation.ErrWhatsAppWebhookSkipped}
	state := newMockWhatsAppSharedState()

	uc := NewConsumeWhatsAppMessageWebhookUseCase(sub, handler, state)
	_ = uc.Start()

	payload := makeStatusPayloadWA("wamid.status-failed", "failed", "5511999999999")

	ack1 := &mockMessageAck{}
	sub.handler(payload, ack1)
	time.Sleep(100 * time.Millisecond)

	ack2 := &mockMessageAck{}
	sub.handler(payload, ack2)
	time.Sleep(100 * time.Millisecond)

	if handler.callCount() != 2 {
		t.Fatalf("failed status event should not be deduped, handler called %d times", handler.callCount())
	}
	if !ack1.wasAcked() || !ack2.wasAcked() {
		t.Fatal("failed status events should be acked after skipped handler execution")
	}
}

func TestConsumeWhatsAppMessage_InvalidJSONNacks(t *testing.T) {
	sub := &mockWhatsAppQueueSub{}
	handler := &mockWhatsAppHandler{}
	state := newMockWhatsAppSharedState()

	uc := NewConsumeWhatsAppMessageWebhookUseCase(sub, handler, state)
	_ = uc.Start()

	ack := &mockMessageAck{}
	sub.handler([]byte("not valid json"), ack)

	time.Sleep(50 * time.Millisecond)

	if handler.callCount() != 0 {
		t.Fatal("handler should not be called for invalid JSON")
	}
	if !ack.wasNacked() {
		t.Fatal("invalid JSON should be nacked")
	}
}

func TestConsumeWhatsAppMessage_HandlerErrorNacksAndAllowsRetry(t *testing.T) {
	sub := &mockWhatsAppQueueSub{}
	pub := &mockWhatsAppQueuePub{}
	handler := &mockWhatsAppHandler{execErrs: []error{errors.New("processing failed"), nil}}
	state := newMockWhatsAppSharedState()

	uc := NewConsumeWhatsAppMessageWebhookUseCaseWithPublisher(sub, pub, handler, state)
	_ = uc.Start()

	payload := makeTextMessagePayload("wamid.err", "5511999999999", "hello")

	ack1 := &mockMessageAck{}
	sub.handler(payload, ack1)
	time.Sleep(100 * time.Millisecond)

	if !ack1.wasAcked() {
		t.Fatal("first delivery must be acked so the consumer slot is freed")
	}
	if ack1.wasNacked() {
		t.Fatal("must not Nack, that's the prod-loop bug")
	}
	if pub.callCount() != 1 {
		t.Fatalf("expected exactly 1 republish to .delay queue, got %d", pub.callCount())
	}
	if pub.firstCall().topic != webhook.TopicWhatsAppMessage {
		t.Fatalf("republish must target main topic, got %q", pub.firstCall().topic)
	}
	if pub.firstCall().delay <= 0 {
		t.Fatalf("republish must use positive delay, got %v", pub.firstCall().delay)
	}

	ack2 := &mockMessageAck{deliveryCount: 2}
	sub.handler(payload, ack2)
	time.Sleep(100 * time.Millisecond)

	if !ack2.wasAcked() {
		t.Fatal("retried message should be acked after succeeding")
	}
	if pub.callCount() != 1 {
		t.Fatalf("successful retry must NOT republish, total calls = %d", pub.callCount())
	}
	if handler.callCount() != 2 {
		t.Fatalf("expected handler to be called twice across retry, got %d", handler.callCount())
	}
}

func TestConsumeWhatsAppMessage_SkippedErrorStillAcks(t *testing.T) {
	sub := &mockWhatsAppQueueSub{}
	handler := &mockWhatsAppHandler{execErr: conversation.ErrWhatsAppWebhookSkipped}
	state := newMockWhatsAppSharedState()

	uc := NewConsumeWhatsAppMessageWebhookUseCase(sub, handler, state)
	_ = uc.Start()

	ack := &mockMessageAck{}
	sub.handler(makeTextMessagePayload("wamid.skip", "5511999999999", "hello"), ack)

	time.Sleep(100 * time.Millisecond)

	if !ack.wasAcked() {
		t.Fatal("skipped messages should still be acked")
	}
}

func TestConsumeWhatsAppMessage_DedupAcquireFailureNacksForRetry(t *testing.T) {
	sub := &mockWhatsAppQueueSub{}
	handler := &mockWhatsAppHandler{}
	state := newMockWhatsAppSharedState()
	state.existsErr = errors.New("redis down")

	uc := NewConsumeWhatsAppMessageWebhookUseCase(sub, handler, state)
	_ = uc.Start()

	ack := &mockMessageAck{}
	sub.handler(makeTextMessagePayload("wamid.redis-err", "5511999999999", "hello"), ack)

	time.Sleep(50 * time.Millisecond)

	if handler.callCount() != 0 {
		t.Fatal("handler should not run when dedupe state cannot be acquired")
	}
	if !ack.wasNacked() || !ack.wasRequeued() {
		t.Fatal("dedupe acquisition failure should nack with requeue")
	}
}

func TestConsumeWhatsAppMessage_InProgressDuplicateNacksForRetry(t *testing.T) {
	sub := &mockWhatsAppQueueSub{}
	handler := &mockWhatsAppHandler{}
	state := newMockWhatsAppSharedState()
	state.store["dedup:webhook:processing:wa:msg:wamid.in-progress"] = true

	uc := NewConsumeWhatsAppMessageWebhookUseCase(sub, handler, state)
	_ = uc.Start()

	ack := &mockMessageAck{}
	sub.handler(makeTextMessagePayload("wamid.in-progress", "5511999999999", "hello"), ack)

	time.Sleep(50 * time.Millisecond)

	if handler.callCount() != 0 {
		t.Fatal("handler should not run while another replica is processing the same message")
	}
	if !ack.wasNacked() || !ack.wasRequeued() {
		t.Fatal("in-progress duplicate should nack with requeue")
	}
}

func TestConsumeWhatsAppMessage_InProgressDuplicateDelaysRetryWhenPublisherConfigured(t *testing.T) {
	sub := &mockWhatsAppQueueSub{}
	pub := &mockWhatsAppQueuePub{}
	handler := &mockWhatsAppHandler{}
	state := newMockWhatsAppSharedState()
	state.store["dedup:webhook:processing:wa:msg:wamid.in-progress"] = true

	uc := NewConsumeWhatsAppMessageWebhookUseCaseWithPublisher(sub, pub, handler, state)
	_ = uc.Start()

	ack := &mockMessageAck{deliveryCount: 2}
	payload := makeTextMessagePayload("wamid.in-progress", "5511999999999", "hello")
	sub.handler(payload, ack)

	time.Sleep(50 * time.Millisecond)

	if handler.callCount() != 0 {
		t.Fatal("handler should not run while another replica is processing the same message")
	}
	if !ack.wasAcked() {
		t.Fatal("in-progress duplicate should ack after scheduling delayed retry")
	}
	if ack.wasNacked() {
		t.Fatal("in-progress duplicate should not nack when delayed retry is scheduled")
	}
	if pub.callCount() != 1 {
		t.Fatalf("expected 1 delayed publish, got %d", pub.callCount())
	}
	call := pub.firstCall()
	if call.topic != webhook.TopicWhatsAppMessage {
		t.Fatalf("expected delayed publish on topic %s, got %s", webhook.TopicWhatsAppMessage, call.topic)
	}
	if call.delay != 4*time.Second {
		t.Fatalf("expected retry delay 4s, got %v", call.delay)
	}
	if string(call.message) != string(payload) {
		t.Fatal("expected delayed publish payload to match original message")
	}
}

func TestConsumeWhatsAppMessage_InProgressDuplicateFallsBackToImmediateRequeueWhenDelayPublishFails(t *testing.T) {
	sub := &mockWhatsAppQueueSub{}
	pub := &mockWhatsAppQueuePub{publishErr: errors.New("publisher down")}
	handler := &mockWhatsAppHandler{}
	state := newMockWhatsAppSharedState()
	state.store["dedup:webhook:processing:wa:msg:wamid.in-progress"] = true

	uc := NewConsumeWhatsAppMessageWebhookUseCaseWithPublisher(sub, pub, handler, state)
	_ = uc.Start()

	ack := &mockMessageAck{}
	sub.handler(makeTextMessagePayload("wamid.in-progress", "5511999999999", "hello"), ack)

	time.Sleep(50 * time.Millisecond)

	if !ack.wasNacked() || !ack.wasRequeued() {
		t.Fatal("consumer should fall back to immediate requeue when delayed publish fails")
	}
	if pub.callCount() != 0 {
		t.Fatalf("expected no successful delayed publish calls, got %d", pub.callCount())
	}
}

// --- Per-sender in-flight lock (safe concurrency / prefetch>1) ---

func TestConsumeWhatsAppMessage_SameSenderInFlightDelaysRetry(t *testing.T) {
	sub := &mockWhatsAppQueueSub{}
	pub := &mockWhatsAppQueuePub{}
	handler := &mockWhatsAppHandler{}
	state := newMockWhatsAppSharedState()
	// Simulate another message from this sender already being processed.
	state.store["inflight:wa:5511999999999"] = true

	uc := NewConsumeWhatsAppMessageWebhookUseCaseWithPublisher(sub, pub, handler, state)
	_ = uc.Start()

	ack := &mockMessageAck{deliveryCount: 2}
	sub.handler(makeTextMessagePayload("wamid.same-sender-2", "5511999999999", "hi"), ack)
	time.Sleep(100 * time.Millisecond)

	if handler.callCount() != 0 {
		t.Fatalf("handler must NOT run while another message from the same sender is in flight, got %d", handler.callCount())
	}
	if !ack.wasAcked() {
		t.Fatal("same-sender-busy message should ack after scheduling a delayed retry")
	}
	if ack.wasNacked() {
		t.Fatal("same-sender-busy message should not nack when a delayed retry is scheduled")
	}
	if pub.callCount() != 1 {
		t.Fatalf("expected exactly 1 delayed republish, got %d", pub.callCount())
	}
	if pub.firstCall().topic != webhook.TopicWhatsAppMessage || pub.firstCall().delay <= 0 {
		t.Fatalf("expected positive-delay republish on main topic, got %+v", pub.firstCall())
	}
}

func TestConsumeWhatsAppMessage_SameSenderInFlightImmediateRequeueWithoutPublisher(t *testing.T) {
	sub := &mockWhatsAppQueueSub{}
	handler := &mockWhatsAppHandler{}
	state := newMockWhatsAppSharedState()
	state.store["inflight:wa:5511999999999"] = true

	uc := NewConsumeWhatsAppMessageWebhookUseCase(sub, handler, state) // no publisher
	_ = uc.Start()

	ack := &mockMessageAck{}
	sub.handler(makeTextMessagePayload("wamid.same-sender-nopub", "5511999999999", "hi"), ack)
	time.Sleep(100 * time.Millisecond)

	if handler.callCount() != 0 {
		t.Fatalf("handler must not run while same sender is in flight, got %d", handler.callCount())
	}
	if !ack.wasNacked() || !ack.wasRequeued() {
		t.Fatal("without a publisher, same-sender-busy message should nack+requeue")
	}
}

func TestConsumeWhatsAppMessage_DifferentSenderNotBlocked(t *testing.T) {
	sub := &mockWhatsAppQueueSub{}
	handler := &mockWhatsAppHandler{}
	state := newMockWhatsAppSharedState()
	state.store["inflight:wa:5511000000000"] = true // a DIFFERENT sender is busy

	uc := NewConsumeWhatsAppMessageWebhookUseCase(sub, handler, state)
	_ = uc.Start()

	ack := &mockMessageAck{}
	sub.handler(makeTextMessagePayload("wamid.other-sender", "5511999999999", "hi"), ack)
	time.Sleep(100 * time.Millisecond)

	if handler.callCount() != 1 {
		t.Fatalf("a message from a different sender must not be blocked, got %d", handler.callCount())
	}
	if !ack.wasAcked() {
		t.Fatal("message from a free sender should be acked after processing")
	}
}

func TestConsumeWhatsAppMessage_StatusEventNotBlockedByUnrelatedSenderLock(t *testing.T) {
	sub := &mockWhatsAppQueueSub{}
	handler := &mockWhatsAppHandler{}
	state := newMockWhatsAppSharedState()
	// A sender's in-flight lock must not block an unrelated status event: status
	// events serialize on their own per-event key (wamid+status), not the sender.
	state.store["inflight:wa:5511999999999"] = true

	uc := NewConsumeWhatsAppMessageWebhookUseCase(sub, handler, state)
	_ = uc.Start()

	ack := &mockMessageAck{}
	sub.handler(makeStatusPayloadWA("wamid.status-nolock", "delivered", "5511999999999"), ack)
	time.Sleep(100 * time.Millisecond)

	if handler.callCount() != 1 {
		t.Fatalf("status event must not be blocked by an unrelated sender lock, got %d", handler.callCount())
	}
	if !ack.wasAcked() {
		t.Fatal("status event should be acked")
	}
}

// Critical safety property: failed-status webhooks are NOT deduped (they trigger
// refunds), so under prefetch>1 a duplicate failed webhook for the same message
// must be serialized by the in-flight lock, otherwise the refund guard could run
// concurrently and double-refund (money leak).
func TestConsumeWhatsAppMessage_DuplicateFailedStatusSerializedByInFlightLock(t *testing.T) {
	sub := &mockWhatsAppQueueSub{}
	pub := &mockWhatsAppQueuePub{}
	handler := &mockWhatsAppHandler{}
	state := newMockWhatsAppSharedState()
	// Simulate the same failed-status event already being processed. The lock key
	// is per status event: "inflight:wa:" + extractWhatsAppMessageID.
	state.store["inflight:wa:status:wamid.failed-dup:failed"] = true

	uc := NewConsumeWhatsAppMessageWebhookUseCaseWithPublisher(sub, pub, handler, state)
	_ = uc.Start()

	ack := &mockMessageAck{deliveryCount: 2}
	sub.handler(makeStatusPayloadWA("wamid.failed-dup", "failed", "5511999999999"), ack)
	time.Sleep(100 * time.Millisecond)

	if handler.callCount() != 0 {
		t.Fatalf("duplicate failed-status webhook must NOT run while the same event is in flight (prevents double refund), got %d", handler.callCount())
	}
	if !ack.wasAcked() || ack.wasNacked() {
		t.Fatal("duplicate failed-status should ack after scheduling a delayed retry")
	}
	if pub.callCount() != 1 {
		t.Fatalf("expected 1 delayed republish for the serialized failed-status duplicate, got %d", pub.callCount())
	}
}

func TestConsumeWhatsAppMessage_SenderLockReleasedAfterProcessing(t *testing.T) {
	sub := &mockWhatsAppQueueSub{}
	handler := &mockWhatsAppHandler{}
	state := newMockWhatsAppSharedState()

	uc := NewConsumeWhatsAppMessageWebhookUseCase(sub, handler, state)
	_ = uc.Start()

	ack1 := &mockMessageAck{}
	sub.handler(makeTextMessagePayload("wamid.rel-first", "5511999999999", "one"), ack1)
	time.Sleep(100 * time.Millisecond)

	state.mu.Lock()
	_, stillLocked := state.store["inflight:wa:5511999999999"]
	state.mu.Unlock()
	if stillLocked {
		t.Fatal("sender lock must be released after the message finishes processing")
	}

	// A second, different message from the same sender must now proceed.
	ack2 := &mockMessageAck{}
	sub.handler(makeTextMessagePayload("wamid.rel-second", "5511999999999", "two"), ack2)
	time.Sleep(100 * time.Millisecond)

	if handler.callCount() != 2 {
		t.Fatalf("second message from same sender should process after lock release, got %d", handler.callCount())
	}
}

func TestExtractWhatsAppMessageID_TextMessage(t *testing.T) {
	payload := &conversation.WhatsAppWebhookPayload{
		Entry: []conversation.WhatsAppEntry{
			{
				Changes: []conversation.WhatsAppChange{
					{
						Value: conversation.WhatsAppChangeValue{
							Messages: []conversation.WhatsAppMessage{
								{ID: "wamid.text-123"},
							},
						},
					},
				},
			},
		},
	}
	id := extractWhatsAppMessageID(payload)
	if id != "msg:wamid.text-123" {
		t.Fatalf("expected 'msg:wamid.text-123', got '%s'", id)
	}
}

func TestExtractWhatsAppMessageID_AudioMessage(t *testing.T) {
	payload := &conversation.WhatsAppWebhookPayload{
		Entry: []conversation.WhatsAppEntry{
			{
				Changes: []conversation.WhatsAppChange{
					{
						Value: conversation.WhatsAppChangeValue{
							Messages: []conversation.WhatsAppMessage{
								{
									ID:    "wamid.audio-123",
									Audio: &conversation.WhatsAppAudioMessage{ID: "audio-media-id"},
								},
							},
						},
					},
				},
			},
		},
	}
	id := extractWhatsAppMessageID(payload)
	if id != "audio:audio-media-id" {
		t.Fatalf("expected 'audio:audio-media-id', got '%s'", id)
	}
}

func TestExtractWhatsAppMessageID_StatusEvent(t *testing.T) {
	payload := &conversation.WhatsAppWebhookPayload{
		Entry: []conversation.WhatsAppEntry{
			{
				Changes: []conversation.WhatsAppChange{
					{
						Value: conversation.WhatsAppChangeValue{
							Statuses: []conversation.WhatsAppStatus{
								{ID: "wamid.status-1", Status: "delivered"},
							},
						},
					},
				},
			},
		},
	}
	id := extractWhatsAppMessageID(payload)
	if id != "status:wamid.status-1:delivered" {
		t.Fatalf("expected 'status:wamid.status-1:delivered', got '%s'", id)
	}
}

func TestExtractWhatsAppMessageID_DifferentStatusesProduceDifferentIDs(t *testing.T) {
	statuses := []struct {
		status string
		want   string
	}{
		{"sent", "status:wamid.same:sent"},
		{"delivered", "status:wamid.same:delivered"},
		{"read", "status:wamid.same:read"},
	}

	for _, tc := range statuses {
		payload := &conversation.WhatsAppWebhookPayload{
			Entry: []conversation.WhatsAppEntry{
				{
					Changes: []conversation.WhatsAppChange{
						{
							Value: conversation.WhatsAppChangeValue{
								Statuses: []conversation.WhatsAppStatus{
									{ID: "wamid.same", Status: tc.status},
								},
							},
						},
					},
				},
			},
		}
		id := extractWhatsAppMessageID(payload)
		if id != tc.want {
			t.Fatalf("status=%s: expected '%s', got '%s'", tc.status, tc.want, id)
		}
	}
}

func TestExtractWhatsAppMessageID_NilPayload(t *testing.T) {
	id := extractWhatsAppMessageID(nil)
	if id != "" {
		t.Fatalf("expected empty for nil payload, got '%s'", id)
	}
}

func TestExtractWhatsAppMessageID_EmptyPayload(t *testing.T) {
	payload := &conversation.WhatsAppWebhookPayload{}
	id := extractWhatsAppMessageID(payload)
	if id != "" {
		t.Fatalf("expected empty for empty payload, got '%s'", id)
	}
}

func TestExtractWhatsAppMessageID_NoMessagesNoStatuses(t *testing.T) {
	payload := &conversation.WhatsAppWebhookPayload{
		Entry: []conversation.WhatsAppEntry{
			{
				Changes: []conversation.WhatsAppChange{
					{
						Value: conversation.WhatsAppChangeValue{},
					},
				},
			},
		},
	}
	id := extractWhatsAppMessageID(payload)
	if id != "" {
		t.Fatalf("expected empty when no messages and no statuses, got '%s'", id)
	}
}

func TestExtractWhatsAppMessageID_MessageTakesPriorityOverStatus(t *testing.T) {
	payload := &conversation.WhatsAppWebhookPayload{
		Entry: []conversation.WhatsAppEntry{
			{
				Changes: []conversation.WhatsAppChange{
					{
						Value: conversation.WhatsAppChangeValue{
							Messages: []conversation.WhatsAppMessage{
								{ID: "wamid.msg-first"},
							},
							Statuses: []conversation.WhatsAppStatus{
								{ID: "wamid.status-second", Status: "sent"},
							},
						},
					},
				},
			},
		},
	}
	id := extractWhatsAppMessageID(payload)
	if id != "msg:wamid.msg-first" {
		t.Fatalf("messages should take priority over statuses, got '%s'", id)
	}
}

func TestExtractSenderPhone(t *testing.T) {
	tests := []struct {
		name    string
		payload *conversation.WhatsAppWebhookPayload
		want    string
	}{
		{"nil payload", nil, ""},
		{"empty payload", &conversation.WhatsAppWebhookPayload{}, ""},
		{"with sender", &conversation.WhatsAppWebhookPayload{
			Entry: []conversation.WhatsAppEntry{{
				Changes: []conversation.WhatsAppChange{{
					Value: conversation.WhatsAppChangeValue{
						Messages: []conversation.WhatsAppMessage{
							{From: "5511999998888", ID: "wamid.1", Type: "text"},
						},
					},
				}},
			}},
		}, "5511999998888"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := extractSenderPhone(tc.payload)
			if got != tc.want {
				t.Fatalf("expected %q, got %q", tc.want, got)
			}
		})
	}
}

func TestFloodDetection_ContextFlags(t *testing.T) {
	ctx := context.Background()
	if IsFloodDetected(ctx) {
		t.Fatal("clean context should not be flood-detected")
	}
	ctx = WithFloodDetected(ctx)
	if !IsFloodDetected(ctx) {
		t.Fatal("context after WithFloodDetected should be flood-detected")
	}
}

func TestConsumeWhatsAppMessage_HandlerErrorBelowMaxRetriesRequeues(t *testing.T) {
	sub := &mockWhatsAppQueueSub{}
	pub := &mockWhatsAppQueuePub{}
	handler := &mockWhatsAppHandler{execErr: errors.New("transient failure")}
	state := newMockWhatsAppSharedState()

	uc := NewConsumeWhatsAppMessageWebhookUseCaseWithPublisher(sub, pub, handler, state)
	_ = uc.Start()

	for attempt := 1; attempt < messaging.MaxRetries; attempt++ {
		callsBefore := pub.callCount()
		ack := &mockMessageAck{}
		sub.handler(makeTextMessagePayload("wamid.cap-below", "5511999999999", "hi"), ack)
		time.Sleep(100 * time.Millisecond)

		if !ack.wasAcked() {
			t.Fatalf("attempt %d: original delivery must be acked so the consumer slot is freed (preventing tenant starvation)", attempt)
		}
		if ack.wasNacked() {
			t.Fatalf("attempt %d: must NOT Nack, Nack(requeue=true) is the bug that caused the prod loop", attempt)
		}
		if pub.callCount() != callsBefore+1 {
			t.Fatalf("attempt %d: expected exactly one delayed republish, got %d new calls", attempt, pub.callCount()-callsBefore)
		}
		last := pub.calls[len(pub.calls)-1]
		if last.topic != webhook.TopicWhatsAppMessage {
			t.Fatalf("attempt %d: expected republish to %s, got %s", attempt, webhook.TopicWhatsAppMessage, last.topic)
		}
		if last.delay <= 0 {
			t.Fatalf("attempt %d: republish must use a positive delay to avoid hot-loop, got %v", attempt, last.delay)
		}
	}
}

func TestConsumeWhatsAppMessage_HandlerErrorAtMaxRetriesIsDropped(t *testing.T) {
	sub := &mockWhatsAppQueueSub{}
	pub := &mockWhatsAppQueuePub{}
	handler := &mockWhatsAppHandler{execErr: errors.New("permanent failure")}
	state := newMockWhatsAppSharedState()

	state.counters["wa:retry:msg:wamid.cap-at"] = int64(messaging.MaxRetries) - 1

	uc := NewConsumeWhatsAppMessageWebhookUseCaseWithPublisher(sub, pub, handler, state)
	_ = uc.Start()

	ack := &mockMessageAck{}
	sub.handler(makeTextMessagePayload("wamid.cap-at", "5511999999999", "hi"), ack)
	time.Sleep(100 * time.Millisecond)

	if !ack.wasAcked() {
		t.Fatal("at MaxRetries the message must be acked (permanently dropped), leaving it unacked starves the channel")
	}
	if ack.wasNacked() {
		t.Fatal("must not Nack at MaxRetries, that resurrects the loop")
	}
	if pub.callCount() != 0 {
		t.Fatalf("at MaxRetries the message must NOT be republished, got %d calls", pub.callCount())
	}

	state.mu.Lock()
	_, counterStillPresent := state.counters["wa:retry:msg:wamid.cap-at"]
	state.mu.Unlock()
	if counterStillPresent {
		t.Fatal("retry counter must be cleared after permanent drop")
	}
}

func TestConsumeWhatsAppMessage_HandlerErrorAboveMaxRetriesIsDropped(t *testing.T) {
	sub := &mockWhatsAppQueueSub{}
	pub := &mockWhatsAppQueuePub{}
	handler := &mockWhatsAppHandler{execErr: errors.New("permanent failure")}
	state := newMockWhatsAppSharedState()

	state.counters["wa:retry:msg:wamid.cap-above"] = int64(messaging.MaxRetries) + 5

	uc := NewConsumeWhatsAppMessageWebhookUseCaseWithPublisher(sub, pub, handler, state)
	_ = uc.Start()

	ack := &mockMessageAck{}
	sub.handler(makeTextMessagePayload("wamid.cap-above", "5511999999999", "hi"), ack)
	time.Sleep(100 * time.Millisecond)

	if !ack.wasAcked() {
		t.Fatal("above MaxRetries the message must be acked + dropped")
	}
	if ack.wasNacked() {
		t.Fatal("must not Nack above MaxRetries")
	}
	if pub.callCount() != 0 {
		t.Fatalf("above MaxRetries the message must NOT be republished, got %d calls", pub.callCount())
	}
}

func TestConsumeWhatsAppMessage_UnknownPhoneAudioDoesNotLoopForever(t *testing.T) {
	sub := &mockWhatsAppQueueSub{}
	pub := &mockWhatsAppQueuePub{}
	handler := &mockWhatsAppHandler{
		execErr: errors.New(`cannot process audio: cannot resolve WhatsApp client: no valid business phone (campaign="", received="")`),
	}
	state := newMockWhatsAppSharedState()

	uc := NewConsumeWhatsAppMessageWebhookUseCaseWithPublisher(sub, pub, handler, state)
	_ = uc.Start()

	payload := makeAudioMessagePayload(
		"wamid.HBgMNTUzMTk1MzY1MjA0FQIAEhggQUM5RDY1MzNFMEVEMzM4QTczREY0NjY5Mjc2RTA3NUUA",
		"1691713198524172",
		"553195365204",
	)

	republishCount := 0

	for attempt := 1; attempt <= messaging.MaxRetries; attempt++ {
		callsBefore := pub.callCount()
		ack := &mockMessageAck{}
		sub.handler(payload, ack)
		time.Sleep(100 * time.Millisecond)

		if ack.wasNacked() {
			t.Fatalf("attempt %d: must NEVER Nack, Nack(requeue=true) is the prod bug", attempt)
		}
		if !ack.wasAcked() {
			t.Fatalf("attempt %d: original delivery must always be acked (either after republish or drop)", attempt)
		}

		newCalls := pub.callCount() - callsBefore
		if attempt < messaging.MaxRetries {
			if newCalls != 1 {
				t.Fatalf("attempt %d (below MaxRetries=%d): expected exactly 1 delayed republish, got %d", attempt, messaging.MaxRetries, newCalls)
			}
			republishCount++
		} else {
			if newCalls != 0 {
				t.Fatalf("attempt %d (==MaxRetries=%d): message must be DROPPED, not republished, got %d new calls", attempt, messaging.MaxRetries, newCalls)
			}
		}
	}

	if republishCount != messaging.MaxRetries-1 {
		t.Fatalf("expected %d republishes, got %d", messaging.MaxRetries-1, republishCount)
	}
	if handler.callCount() != messaging.MaxRetries {
		t.Fatalf("expected handler invoked %d times (one per delivery, then dropped), got %d", messaging.MaxRetries, handler.callCount())
	}

	for i, c := range pub.calls {
		if c.topic != webhook.TopicWhatsAppMessage {
			t.Fatalf("republish %d targeted wrong topic %q", i, c.topic)
		}
		if c.delay <= 0 {
			t.Fatalf("republish %d had non-positive delay %v, would resurrect the hot loop", i, c.delay)
		}
	}
}

func TestConsumeWhatsAppMessage_FailingMessageDoesNotBlockNextMessage(t *testing.T) {
	sub := &mockWhatsAppQueueSub{}
	pub := &mockWhatsAppQueuePub{}
	failures := make([]error, messaging.MaxRetries)
	for i := range failures {
		failures[i] = errors.New("cannot resolve WhatsApp client: no valid business phone")
	}
	handler := &mockWhatsAppHandler{execErrs: failures}
	state := newMockWhatsAppSharedState()

	uc := NewConsumeWhatsAppMessageWebhookUseCaseWithPublisher(sub, pub, handler, state)
	_ = uc.Start()

	poison := makeAudioMessagePayload("wamid.poison", "audio-poison", "553195365204")
	for attempt := 1; attempt <= messaging.MaxRetries; attempt++ {
		ack := &mockMessageAck{}
		sub.handler(poison, ack)
		time.Sleep(100 * time.Millisecond)
		if !ack.wasAcked() {
			t.Fatalf("attempt %d: poison pill must always be acked so the consumer slot is freed", attempt)
		}
	}

	statusAck := &mockMessageAck{}
	sub.handler(makeStatusPayloadWA("wamid.legit", "delivered", "5511999999999"), statusAck)
	time.Sleep(100 * time.Millisecond)

	if !statusAck.wasAcked() {
		t.Fatal("legitimate status webhook must be processed after poison pill is acked + dropped")
	}
	if statusAck.wasNacked() {
		t.Fatal("legitimate status webhook should not be nacked")
	}
}

func TestConsumeWhatsAppMessage_NoPublisherDropsOnFirstError(t *testing.T) {
	sub := &mockWhatsAppQueueSub{}
	handler := &mockWhatsAppHandler{execErr: errors.New("transient failure")}
	state := newMockWhatsAppSharedState()

	uc := NewConsumeWhatsAppMessageWebhookUseCase(sub, handler, state)
	_ = uc.Start()

	ack := &mockMessageAck{deliveryCount: 1}
	sub.handler(makeTextMessagePayload("wamid.no-pub", "5511999999999", "hi"), ack)
	time.Sleep(100 * time.Millisecond)

	if !ack.wasAcked() {
		t.Fatal("with no publisher, message must be acked (dropped), fallback to Nack(true) would resurrect the loop")
	}
	if ack.wasNacked() {
		t.Fatal("with no publisher, must not Nack")
	}
}

func TestConsumeWhatsAppMessage_DelayPublishFailureDrops(t *testing.T) {
	sub := &mockWhatsAppQueueSub{}
	pub := &mockWhatsAppQueuePub{publishErr: errors.New("broker unavailable")}
	handler := &mockWhatsAppHandler{execErr: errors.New("transient failure")}
	state := newMockWhatsAppSharedState()

	uc := NewConsumeWhatsAppMessageWebhookUseCaseWithPublisher(sub, pub, handler, state)
	_ = uc.Start()

	ack := &mockMessageAck{}
	sub.handler(makeTextMessagePayload("wamid.broker-down", "5511999999999", "hi"), ack)
	time.Sleep(100 * time.Millisecond)

	if !ack.wasAcked() {
		t.Fatal("with broker republish failure, message must be acked + dropped")
	}
	if ack.wasNacked() {
		t.Fatal("must not Nack on publish failure, that resurrects the starvation loop")
	}
}
