package conversation_usecase

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"vozko/domain/messaging"
	"vozko/domain/workflow"
)

type capturingSub struct {
	messaging.MessageQueueSub
	topic   string
	handler func([]byte, messaging.MessageAck)
}

func (s *capturingSub) Subscribe(topic string, handler func([]byte, messaging.MessageAck)) error {
	s.topic, s.handler = topic, handler
	return nil
}

type recordedAck struct {
	mu      sync.Mutex
	acked   bool
	nacked  bool
	requeue bool
	done    chan struct{}
}

func newRecordedAck() *recordedAck { return &recordedAck{done: make(chan struct{})} }

func (a *recordedAck) Ack() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.acked = true
	close(a.done)
	return nil
}

func (a *recordedAck) Nack(requeue bool) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.nacked, a.requeue = true, requeue
	close(a.done)
	return nil
}

func (a *recordedAck) DeliveryCount() int { return 1 }

type handledEvents struct {
	mu     sync.Mutex
	events []workflow.AgentRunFinished
}

func (h *handledEvents) Handle(_ context.Context, ev workflow.AgentRunFinished) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.events = append(h.events, ev)
	return "sent"
}

func waitAck(t *testing.T, ack *recordedAck) {
	t.Helper()
	select {
	case <-ack.done:
	case <-time.After(time.Second):
		t.Fatal("the message was never settled")
	}
}

func TestTheConsumerHandsEachFinishedAgentRunToTheContinuationAndAcks(t *testing.T) {
	sub, handled := &capturingSub{}, &handledEvents{}
	if err := NewConsumeAgentRunFinished(sub, handled).Start(); err != nil {
		t.Fatal(err)
	}
	if sub.topic != workflow.TopicAgentRunFinished {
		t.Fatalf("topic = %q", sub.topic)
	}

	payload, _ := json.Marshal(workflow.AgentRunFinished{RunID: "run-1", Status: workflow.RunStatusCompleted, HasAgentResult: true})
	ack := newRecordedAck()
	sub.handler(payload, ack)
	waitAck(t, ack)

	if !ack.acked || len(handled.events) != 1 || handled.events[0].RunID != "run-1" {
		t.Fatalf("acked = %v, handled = %+v", ack.acked, handled.events)
	}
}

func TestAnUnreadableEventIsDroppedWithoutRequeue(t *testing.T) {
	sub, handled := &capturingSub{}, &handledEvents{}
	_ = NewConsumeAgentRunFinished(sub, handled).Start()

	ack := newRecordedAck()
	sub.handler([]byte("{"), ack)
	waitAck(t, ack)

	if !ack.nacked || ack.requeue || len(handled.events) != 0 {
		t.Fatalf("nacked = %v requeue = %v handled = %d", ack.nacked, ack.requeue, len(handled.events))
	}
}
