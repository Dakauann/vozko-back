package notification_usecase

import (
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"vozko/domain/messaging"
	"vozko/domain/notification"
)

type stubEmailSvc struct{ err error }

func (s *stubEmailSvc) SendEmail(to, subject, body string) error { return s.err }
func (s *stubEmailSvc) SendTemplate(to, subject, template string, data map[string]interface{}) error {
	return s.err
}

type delayedPublish struct {
	topic   string
	delay   time.Duration
	message []byte
}

type stubPub struct {
	mu         sync.Mutex
	delayed    []delayedPublish
	publishErr error
}

func (p *stubPub) Publish(topic string, message []byte) error { return nil }
func (p *stubPub) PublishWithDelay(topic string, message []byte, delay time.Duration) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.publishErr != nil {
		return p.publishErr
	}
	p.delayed = append(p.delayed, delayedPublish{topic: topic, delay: delay, message: message})
	return nil
}
func (p *stubPub) ValidateConnection() error { return nil }
func (p *stubPub) calls() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.delayed)
}

type stubAck struct {
	deliveryCount int
	mu            sync.Mutex
	acked         bool
	nacked        bool
	requeue       bool
	done          chan struct{}
}

func newStubAck(dc int) *stubAck { return &stubAck{deliveryCount: dc, done: make(chan struct{}, 1)} }
func (a *stubAck) Ack() error {
	a.mu.Lock()
	a.acked = true
	a.mu.Unlock()
	a.signal()
	return nil
}
func (a *stubAck) Nack(requeue bool) error {
	a.mu.Lock()
	a.nacked = true
	a.requeue = requeue
	a.mu.Unlock()
	a.signal()
	return nil
}
func (a *stubAck) DeliveryCount() int { return a.deliveryCount }
func (a *stubAck) signal() {
	select {
	case a.done <- struct{}{}:
	default:
	}
}

type stubMetrics struct {
	mu         sync.Mutex
	sendErrors int
}

func (m *stubMetrics) IncEmailSendError(string) {
	m.mu.Lock()
	m.sendErrors++
	m.mu.Unlock()
}
func (m *stubMetrics) IncMetricsRecordError(string, string) {}
func (m *stubMetrics) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sendErrors
}

func mustMessage(t *testing.T) []byte {
	t.Helper()
	return messageAtAttempt(t, 0)
}

func messageAtAttempt(t *testing.T, attempt int) []byte {
	t.Helper()
	b, err := json.Marshal(notification.NotificationQueueMessage{
		Email:    "user@example.com",
		Subject:  "hi",
		Template: "welcome",
		Attempt:  attempt,
	})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func attemptOf(t *testing.T, raw []byte) int {
	t.Helper()
	var m notification.NotificationQueueMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	return m.Attempt
}

const delayedCopyDeliveryCount = 2

func TestConsumeEmail_RetriesStopAfterMaxBecauseTheAttemptTravelsInTheMessageNotTheBrokerCount(t *testing.T) {
	pub := &stubPub{}
	uc := NewConsumeEmailUseCase(nil, pub, &stubEmailSvc{err: errors.New("rate limit exceeded")}, &stubMetrics{})

	message := mustMessage(t)
	for delivery := 1; delivery <= messaging.MaxRetries+2; delivery++ {
		before := pub.calls()
		ack := newStubAck(delayedCopyDeliveryCount)
		uc.HandleEmailPublication(message, ack)
		waitDone(t, ack)
		if pub.calls() == before {
			if delivery != messaging.MaxRetries {
				t.Fatalf("dropped at delivery %d, want drop exactly at %d", delivery, messaging.MaxRetries)
			}
			return
		}
		message = pub.delayed[pub.calls()-1].message
		if got := attemptOf(t, message); got != delivery {
			t.Fatalf("republished copy carries attempt %d, want %d", got, delivery)
		}
	}
	t.Fatalf("a failing email kept retrying past %d attempts", messaging.MaxRetries)
}

func TestConsumeEmail_PermanentFailuresAreDroppedWithoutRetryBecauseRetryingCannotFixThem(t *testing.T) {
	cases := []string{
		"failed to send email via Resend: [ERROR]: API key is invalid",
		"401 unauthorized",
		"403 forbidden",
		"422 invalid recipient address",
	}
	for _, msg := range cases {
		t.Run(msg, func(t *testing.T) {
			pub := &stubPub{}
			uc := NewConsumeEmailUseCase(nil, pub, &stubEmailSvc{err: errors.New(msg)}, &stubMetrics{})
			ack := newStubAck(1)

			uc.HandleEmailPublication(mustMessage(t), ack)
			waitDone(t, ack)

			if pub.calls() != 0 {
				t.Fatalf("a permanent failure must not be retried, got %d republish", pub.calls())
			}
			if !ack.acked {
				t.Fatal("a permanent failure must be dropped via Ack")
			}
		})
	}
}

func waitDone(t *testing.T, a *stubAck) {
	t.Helper()
	select {
	case <-a.done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for ack/nack")
	}
}

func TestConsumeEmail_Success_Acks(t *testing.T) {
	pub := &stubPub{}
	metrics := &stubMetrics{}
	uc := NewConsumeEmailUseCase(nil, pub, &stubEmailSvc{err: nil}, metrics)
	ack := newStubAck(1)

	uc.HandleEmailPublication(mustMessage(t), ack)
	waitDone(t, ack)

	if !ack.acked || ack.nacked {
		t.Fatalf("success must Ack only: acked=%v nacked=%v", ack.acked, ack.nacked)
	}
	if pub.calls() != 0 {
		t.Fatalf("success must not republish, got %d", pub.calls())
	}
	if metrics.count() != 0 {
		t.Fatalf("success must not record send error, got %d", metrics.count())
	}
}

func TestConsumeEmail_TransientFailure_BelowMax_SchedulesDelayedRetry(t *testing.T) {
	pub := &stubPub{}
	metrics := &stubMetrics{}
	uc := NewConsumeEmailUseCase(nil, pub, &stubEmailSvc{err: errors.New("rate limit exceeded")}, metrics)
	ack := newStubAck(1)

	uc.HandleEmailPublication(mustMessage(t), ack)
	waitDone(t, ack)

	if pub.calls() != 1 {
		t.Fatalf("expected exactly 1 delayed republish, got %d", pub.calls())
	}
	if got := pub.delayed[0]; got.topic != notification.EmailNotificationTopic || got.delay != emailRetryBackoff(1) {
		t.Fatalf("republish = %+v, want topic=%s delay=%v", got, notification.EmailNotificationTopic, emailRetryBackoff(1))
	}
	if !ack.acked || ack.nacked {
		t.Fatalf("after scheduling retry the original must be Acked: acked=%v nacked=%v", ack.acked, ack.nacked)
	}
	if metrics.count() != 1 {
		t.Fatalf("expected 1 recorded send error, got %d", metrics.count())
	}
}

func TestConsumeEmail_AtMaxRetries_DropsWithoutRepublish(t *testing.T) {
	pub := &stubPub{}
	metrics := &stubMetrics{}
	uc := NewConsumeEmailUseCase(nil, pub, &stubEmailSvc{err: errors.New("rate limit exceeded")}, metrics)
	ack := newStubAck(1)

	uc.HandleEmailPublication(messageAtAttempt(t, messaging.MaxRetries-1), ack)
	waitDone(t, ack)

	if pub.calls() != 0 {
		t.Fatalf("at MaxRetries must NOT republish, got %d", pub.calls())
	}
	if !ack.acked {
		t.Fatal("at MaxRetries the message must be dropped via Ack (not left unacked)")
	}
}

func TestConsumeEmail_PublishError_RequeuesInPlace(t *testing.T) {
	pub := &stubPub{publishErr: errors.New("broker down")}
	metrics := &stubMetrics{}
	uc := NewConsumeEmailUseCase(nil, pub, &stubEmailSvc{err: errors.New("rate limit exceeded")}, metrics)
	ack := newStubAck(1)

	uc.HandleEmailPublication(mustMessage(t), ack)
	waitDone(t, ack)

	if !ack.nacked || !ack.requeue {
		t.Fatalf("when delayed publish fails, must Nack(requeue=true) so the email is not lost: nacked=%v requeue=%v", ack.nacked, ack.requeue)
	}
}

func TestConsumeEmail_MalformedPayload_Dropped(t *testing.T) {
	uc := NewConsumeEmailUseCase(nil, &stubPub{}, &stubEmailSvc{}, &stubMetrics{})
	ack := newStubAck(1)

	uc.HandleEmailPublication([]byte("not json"), ack)
	waitDone(t, ack)

	if !ack.nacked || ack.requeue {
		t.Fatalf("malformed payload must Nack(false) to drop: nacked=%v requeue=%v", ack.nacked, ack.requeue)
	}
}

func TestExtractErrorType_RateLimit(t *testing.T) {
	if got := extractErrorType(errors.New("failed to send email via Resend after 4 attempts: rate limit exceeded")); got != "rate_limit" {
		t.Fatalf("extractErrorType = %q, want rate_limit", got)
	}
}
