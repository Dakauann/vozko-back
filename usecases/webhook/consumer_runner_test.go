package webhook_usecase

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"vozko/domain/messaging"
)

// spinAck models RabbitMQ's actual behaviour, which is the whole point of these
// tests: DeliveryCount() is read from the x-death header, and RabbitMQ only stamps
// x-death when a message is genuinely dead-lettered. A message put back by
// Nack(requeue=true) carries no such header, so its count stays at 1 no matter how
// many times it has been tried.
type spinAck struct {
	mu sync.Mutex

	deliveryCount int
	acked         int
	nackRequeue   int
	nackDrop      int

	// dispatch handles in a goroutine, so tests must wait for a terminal
	// acknowledgement rather than asserting straight after the call.
	once sync.Once
	done chan struct{}
}

func newSpinAck(deliveryCount int) *spinAck {
	return &spinAck{deliveryCount: deliveryCount, done: make(chan struct{})}
}

// settle blocks until the message has been acked or nacked.
func (a *spinAck) settle(t *testing.T) {
	t.Helper()
	select {
	case <-a.done:
	case <-time.After(2 * time.Second):
		t.Fatal("message was never acked or nacked")
	}
}

func (a *spinAck) finish() { a.once.Do(func() { close(a.done) }) }

func (a *spinAck) Ack() error {
	a.mu.Lock()
	a.acked++
	a.mu.Unlock()
	a.finish()
	return nil
}

func (a *spinAck) Nack(requeue bool) error {
	a.mu.Lock()
	if requeue {
		a.nackRequeue++
	} else {
		a.nackDrop++
	}
	a.mu.Unlock()
	a.finish()
	return nil
}

func (a *spinAck) DeliveryCount() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.deliveryCount < 1 {
		return 1
	}
	return a.deliveryCount
}

type delayedMessage struct {
	topic   string
	payload []byte
	delay   time.Duration
}

// recordingPub separates plain publishes (DLQ) from delayed ones (retries), which
// the shared mockQueuePub deliberately conflates.
type recordingPub struct {
	mu      sync.Mutex
	plain   []publishedMessage
	delayed []delayedMessage
	err     error
}

func (p *recordingPub) Publish(topic string, message []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.err != nil {
		return p.err
	}
	p.plain = append(p.plain, publishedMessage{topic: topic, payload: message})
	return nil
}

func (p *recordingPub) PublishWithDelay(topic string, message []byte, delay time.Duration) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.err != nil {
		return p.err
	}
	p.delayed = append(p.delayed, delayedMessage{topic: topic, payload: message, delay: delay})
	return nil
}

func (p *recordingPub) ValidateConnection() error { return nil }

func (p *recordingPub) counts() (plain, delayed int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.plain), len(p.delayed)
}

type testPayload struct {
	ID string `json:"id"`
}

// TestConsumerRunner_RetryableFailureDoesNotSpin is the regression test for a
// production hot loop.
//
// A single message whose handler always failed was retried hundreds of times
// inside one second, at full CPU, and never reached the DLQ. The cause was
// Nack(requeue=true) on a retryable failure: the broker re-delivers immediately
// AND without x-death, so DeliveryCount() reports 1 forever and the
// "retries exhausted" check can never fire.
//
// The invariant: a retryable failure must never requeue directly. It must go
// through the delay queue, which is what advances the attempt counter.
func TestConsumerRunner_RetryableFailureDoesNotSpin(t *testing.T) {
	pub := &recordingPub{}
	var (
		mu    sync.Mutex
		calls int
	)

	runner := NewConsumerRunner(ConsumerConfig[testPayload]{
		Name:     "test-consumer",
		Topic:    "webhook.test",
		QueuePub: pub,
		Handle: func(context.Context, *testPayload) error {
			mu.Lock()
			calls++
			mu.Unlock()
			return errors.New("database is unreachable")
		},
	})

	ack := newSpinAck(1)
	runner.dispatch([]byte(`{"id":"m1"}`), ack)
	ack.settle(t)

	mu.Lock()
	defer mu.Unlock()
	if calls != 1 {
		t.Fatalf("handler called %d times, want exactly 1 per delivery", calls)
	}

	ack.mu.Lock()
	requeues := ack.nackRequeue
	ack.mu.Unlock()
	if requeues != 0 {
		t.Errorf("Nack(requeue=true) called %d times; an immediate requeue carries no "+
			"x-death, so the attempt counter never advances and the message spins", requeues)
	}

	plain, delayed := pub.counts()
	if delayed != 1 {
		t.Fatalf("delayed republishes = %d, want 1 (the retry must go through the delay queue)", delayed)
	}
	if plain != 0 {
		t.Errorf("plain publishes = %d, want 0 (nothing should be dead-lettered on attempt 1)", plain)
	}
	if pub.delayed[0].delay <= 0 {
		t.Errorf("retry delay = %v, want a positive backoff", pub.delayed[0].delay)
	}
	if pub.delayed[0].topic != "webhook.test" {
		t.Errorf("retry topic = %q, want the original topic", pub.delayed[0].topic)
	}
}

// TestConsumerRunner_ExhaustedRetriesDeadLetter: once the attempt counter has
// actually advanced (which only the delay-queue path makes possible), the message
// must be parked rather than retried forever.
func TestConsumerRunner_ExhaustedRetriesDeadLetter(t *testing.T) {
	pub := &recordingPub{}

	runner := NewConsumerRunner(ConsumerConfig[testPayload]{
		Name:     "test-consumer",
		Topic:    "webhook.test",
		QueuePub: pub,
		Handle: func(context.Context, *testPayload) error {
			return errors.New("still broken")
		},
	})

	ack := newSpinAck(messaging.MaxRetries)
	runner.dispatch([]byte(`{"id":"m1"}`), ack)
	ack.settle(t)

	plain, delayed := pub.counts()
	if plain != 1 {
		t.Fatalf("DLQ publishes = %d, want 1 after %d attempts", plain, messaging.MaxRetries)
	}
	if pub.plain[0].topic != "webhook.test"+messaging.DLQSuffix {
		t.Errorf("DLQ topic = %q, want %q", pub.plain[0].topic, "webhook.test"+messaging.DLQSuffix)
	}
	if delayed != 0 {
		t.Errorf("delayed republishes = %d, want 0 once retries are exhausted", delayed)
	}
}

// TestConsumerRunner_NoPublisherParksInsteadOfSpinning covers the fallback path.
// Without a publisher there is no way to delay, and an immediate requeue would
// reintroduce the spin — so the message is parked instead.
func TestConsumerRunner_NoPublisherParksInsteadOfSpinning(t *testing.T) {
	runner := NewConsumerRunner(ConsumerConfig[testPayload]{
		Name:  "test-consumer",
		Topic: "webhook.test",
		Handle: func(context.Context, *testPayload) error {
			return errors.New("broken")
		},
	})

	ack := newSpinAck(1)
	runner.dispatch([]byte(`{"id":"m1"}`), ack)
	ack.settle(t)

	ack.mu.Lock()
	defer ack.mu.Unlock()
	if ack.nackRequeue != 0 {
		t.Errorf("Nack(requeue=true) called %d times with no publisher; that is the spin", ack.nackRequeue)
	}
	if ack.acked != 1 {
		t.Errorf("acked %d times, want 1 (parked)", ack.acked)
	}
}

// TestConsumerRunner_UnparseablePayloadIsDropped: a payload that cannot be decoded
// will never decode, so requeueing it is a guaranteed infinite loop.
func TestConsumerRunner_UnparseablePayloadIsDropped(t *testing.T) {
	pub := &recordingPub{}
	var called atomic.Bool

	runner := NewConsumerRunner(ConsumerConfig[testPayload]{
		Name:     "test-consumer",
		Topic:    "webhook.test",
		QueuePub: pub,
		Handle: func(context.Context, *testPayload) error {
			called.Store(true)
			return nil
		},
	})

	ack := newSpinAck(1)
	runner.dispatch([]byte(`{not json`), ack)
	ack.settle(t)

	if called.Load() {
		t.Error("handler ran on an undecodable payload")
	}
	ack.mu.Lock()
	defer ack.mu.Unlock()
	if ack.nackDrop != 1 {
		t.Errorf("Nack(requeue=false) called %d times, want 1", ack.nackDrop)
	}
	if ack.nackRequeue != 0 {
		t.Errorf("Nack(requeue=true) called %d times; an undecodable payload requeued is an infinite loop", ack.nackRequeue)
	}
}

// TestConsumerRunner_SuccessAcks is the happy path.
func TestConsumerRunner_SuccessAcks(t *testing.T) {
	pub := &recordingPub{}
	var (
		mu  sync.Mutex
		got string
	)

	runner := NewConsumerRunner(ConsumerConfig[testPayload]{
		Name:     "test-consumer",
		Topic:    "webhook.test",
		QueuePub: pub,
		Handle: func(_ context.Context, p *testPayload) error {
			mu.Lock()
			got = p.ID
			mu.Unlock()
			return nil
		},
	})

	ack := newSpinAck(1)
	runner.dispatch([]byte(`{"id":"m42"}`), ack)
	ack.settle(t)

	mu.Lock()
	if got != "m42" {
		t.Errorf("handler saw id %q, want m42", got)
	}
	mu.Unlock()
	ack.mu.Lock()
	defer ack.mu.Unlock()
	if ack.acked != 1 {
		t.Errorf("acked %d times, want 1", ack.acked)
	}
	if ack.nackRequeue != 0 || ack.nackDrop != 0 {
		t.Errorf("nacked on success: requeue=%d drop=%d", ack.nackRequeue, ack.nackDrop)
	}
}

// TestConsumerRunner_DropDispositionAcks: a permanent failure is acked, not
// retried — retrying something that cannot succeed is the spin in another form.
func TestConsumerRunner_DropDispositionAcks(t *testing.T) {
	pub := &recordingPub{}

	runner := NewConsumerRunner(ConsumerConfig[testPayload]{
		Name:     "test-consumer",
		Topic:    "webhook.test",
		QueuePub: pub,
		Handle: func(context.Context, *testPayload) error {
			return errors.New("account was deleted")
		},
		Classify: func(error) Disposition { return DispositionDrop },
	})

	ack := newSpinAck(1)
	runner.dispatch([]byte(`{"id":"m1"}`), ack)
	ack.settle(t)

	plain, delayed := pub.counts()
	if plain != 0 || delayed != 0 {
		t.Errorf("republished a dropped message: plain=%d delayed=%d", plain, delayed)
	}
	ack.mu.Lock()
	defer ack.mu.Unlock()
	if ack.acked != 1 {
		t.Errorf("acked %d times, want 1", ack.acked)
	}
}
