package conversation_usecase

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

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

func waitAck(t *testing.T, ctx context.Context, ack *recordedAck) {
	t.Helper()
	select {
	case <-ack.done:
	case <-ctx.Done():
		t.Fatal("the message was never settled")
	}
}

func settled(ack *recordedAck) bool {
	select {
	case <-ack.done:
		return true
	default:
		return false
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
	waitAck(t, untilSettled(t), ack)

	if !ack.acked || len(handled.events) != 1 || handled.events[0].RunID != "run-1" {
		t.Fatalf("acked = %v, handled = %+v", ack.acked, handled.events)
	}
}

func TestAnUnreadableEventIsDroppedWithoutRequeue(t *testing.T) {
	sub, handled := &capturingSub{}, &handledEvents{}
	_ = NewConsumeAgentRunFinished(sub, handled).Start()

	ack := newRecordedAck()
	sub.handler([]byte("{"), ack)
	waitAck(t, untilSettled(t), ack)

	if !ack.nacked || ack.requeue || len(handled.events) != 0 {
		t.Fatalf("nacked = %v requeue = %v handled = %d", ack.nacked, ack.requeue, len(handled.events))
	}
}

func TestTwoDeliveriesOfOneContinuationOnTheSameConversationNeverRunTwoAgentTurns(t *testing.T) {
	ctx := untilSettled(t)
	h := newContinuationHarness()
	var mu sync.Mutex
	inTurn, mostAtOnce := 0, 0
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	h.channels.onTurn = func() {
		mu.Lock()
		inTurn++
		if inTurn > mostAtOnce {
			mostAtOnce = inTurn
		}
		mu.Unlock()
		entered <- struct{}{}
		<-release
		mu.Lock()
		inTurn--
		mu.Unlock()
	}
	sub := &capturingSub{}
	if err := NewConsumeAgentRunFinished(sub, h.continuation(t)).Start(); err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(finished(workflow.RunStatusCompleted, true))

	first, second := newRecordedAck(), newRecordedAck()
	sub.handler(payload, first)
	sub.handler(payload, second)

	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("no agent turn started")
	}
	h.lockState.awaitWaiter(t, ctx)
	if settled(first) || settled(second) {
		t.Fatal("a delivery settled while the agent turn was still running, so the other did not wait for the turn lock")
	}
	close(release)
	waitAck(t, ctx, first)
	waitAck(t, ctx, second)

	h.metrics.mu.Lock()
	outcomes := append([]string(nil), h.metrics.outcomes...)
	h.metrics.mu.Unlock()
	if mostAtOnce != 1 || len(h.turns()) != 1 || len(outcomes) != 2 {
		t.Fatalf("turns at once = %d, turns = %d, outcomes = %v", mostAtOnce, len(h.turns()), outcomes)
	}
	if !(outcomes[0] == "sent" && outcomes[1] == "already_continued") && !(outcomes[1] == "sent" && outcomes[0] == "already_continued") {
		t.Fatalf("outcomes = %v, want one sent and one already_continued", outcomes)
	}
}
