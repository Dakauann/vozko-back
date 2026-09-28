package webhook_usecase

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
)

type stubDurable struct {
	claimed bool
	err     error
}

func (s stubDurable) Claim(context.Context, string, string, string) (bool, error) {
	return s.claimed, s.err
}

func keyedRunner(pub *recordingPub, durable DurableDedup, calls *atomic.Int32) *ConsumerRunner[testPayload] {
	return NewConsumerRunner(ConsumerConfig[testPayload]{
		Name:        "test-consumer",
		Topic:       "webhook.test",
		QueuePub:    pub,
		SharedState: newMockSharedState(),
		Durable:     durable,
		DedupKey:    func(p *testPayload) string { return p.ID },
		Handle: func(context.Context, *testPayload) error {
			calls.Add(1)
			return nil
		},
	})
}

func TestConsumerRunner_DurableFailureRequeuesWithoutHandling(t *testing.T) {
	pub := &recordingPub{}
	var calls atomic.Int32
	runner := keyedRunner(pub, stubDurable{err: errors.New("postgres down")}, &calls)

	ack := newSpinAck(1)
	runner.dispatch([]byte(`{"id":"m1"}`), ack)
	ack.settle(t)

	if calls.Load() != 0 {
		t.Fatal("handler ran although the durable dedup store failed")
	}
	if _, delayed := pub.counts(); delayed != 1 {
		t.Fatalf("delayed requeues = %d, want 1", delayed)
	}
}

func TestConsumerRunner_DurableDuplicateAcksWithoutHandling(t *testing.T) {
	pub := &recordingPub{}
	var calls atomic.Int32
	runner := keyedRunner(pub, stubDurable{claimed: false}, &calls)

	ack := newSpinAck(1)
	runner.dispatch([]byte(`{"id":"m1"}`), ack)
	ack.settle(t)

	if calls.Load() != 0 || ack.acked != 1 {
		t.Fatalf("calls=%d acked=%d", calls.Load(), ack.acked)
	}
}

func TestConsumerRunner_StartRequiresDurableStore(t *testing.T) {
	runner := NewConsumerRunner(ConsumerConfig[testPayload]{
		Name:     "test-consumer",
		Topic:    "webhook.test",
		QueuePub: &recordingPub{},
		DedupKey: func(p *testPayload) string { return p.ID },
		Handle:   func(context.Context, *testPayload) error { return nil },
	})
	if err := runner.Start(); err == nil {
		t.Fatal("a keyed consumer started without a durable dedup store")
	}
}
