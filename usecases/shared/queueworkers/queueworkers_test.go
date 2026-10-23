package queueworkers

import (
	"sync"
	"testing"
	"time"

	"vozko/domain/messaging"
)

type settlement struct {
	mu      sync.Mutex
	acked   bool
	nacked  bool
	requeue bool
	done    chan struct{}
}

func newSettlement() *settlement { return &settlement{done: make(chan struct{})} }

func (s *settlement) Ack() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.acked = true
	close(s.done)
	return nil
}

func (s *settlement) Nack(requeue bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nacked, s.requeue = true, requeue
	close(s.done)
	return nil
}

func (s *settlement) DeliveryCount() int { return 1 }

func (s *settlement) settled() bool {
	select {
	case <-s.done:
		return true
	default:
		return false
	}
}

func waitSettled(t *testing.T, s *settlement) {
	t.Helper()
	select {
	case <-s.done:
	case <-time.After(time.Second):
		t.Fatal("the message was never settled")
	}
}

func TestMessagesRunAtOnceUpToThePoolSizeAndSettleOnlyWhenTheirWorkEnds(t *testing.T) {
	const size = 3
	started := make(chan struct{}, size)
	release := make(chan struct{})
	handler := NewPool(size).Handler(func(_ []byte, ack messaging.MessageAck) {
		started <- struct{}{}
		<-release
		_ = ack.Ack()
	})

	acks := make([]*settlement, size)
	for i := range acks {
		acks[i] = newSettlement()
		handler(nil, acks[i])
	}
	for i := 0; i < size; i++ {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatalf("only %d of %d messages are in flight", i, size)
		}
	}
	for i, ack := range acks {
		if ack.settled() {
			t.Fatalf("message %d was settled before its work finished", i)
		}
	}
	close(release)
	for _, ack := range acks {
		waitSettled(t, ack)
	}
}

func TestADeliveryBeyondThePoolWaitsForAFreeWorker(t *testing.T) {
	release := make(chan struct{})
	handler := NewPool(1).Handler(func(_ []byte, ack messaging.MessageAck) {
		<-release
		_ = ack.Ack()
	})
	first := newSettlement()
	handler(nil, first)

	returned := make(chan struct{})
	second := newSettlement()
	go func() {
		handler(nil, second)
		close(returned)
	}()
	select {
	case <-returned:
		t.Fatal("the second delivery was taken while the only worker was busy")
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	waitSettled(t, first)
	waitSettled(t, second)
}

func TestAPanickingWorkerReturnsItsMessageAndFreesTheSlot(t *testing.T) {
	calls := 0
	var mu sync.Mutex
	handler := NewPool(1).Handler(func(_ []byte, ack messaging.MessageAck) {
		mu.Lock()
		calls++
		first := calls == 1
		mu.Unlock()
		if first {
			panic("boom")
		}
		_ = ack.Ack()
	})
	crashed := newSettlement()
	handler(nil, crashed)
	waitSettled(t, crashed)
	if !crashed.nacked || !crashed.requeue {
		t.Fatalf("panicked message: nacked %v requeue %v, want returned to the queue", crashed.nacked, crashed.requeue)
	}
	next := newSettlement()
	handler(nil, next)
	waitSettled(t, next)
	if !next.acked {
		t.Fatal("the slot was not freed after the panic")
	}
}

func TestAPoolNeverHasFewerThanOneWorker(t *testing.T) {
	ack := newSettlement()
	NewPool(0).Handler(func(_ []byte, ack messaging.MessageAck) { _ = ack.Ack() })(nil, ack)
	waitSettled(t, ack)
}
