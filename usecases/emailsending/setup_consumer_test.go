package emailsending

import (
	"context"
	"encoding/json"
	"testing"

	es "vozko/domain/emailsending"
	"vozko/domain/messaging"
	"vozko/usecases/shared/delayedfire"
)

type topicSub struct {
	handlers map[string]func([]byte, messaging.MessageAck)
}

func (s *topicSub) Subscribe(topic string, handler func([]byte, messaging.MessageAck)) error {
	s.handlers[topic] = handler
	return nil
}

func (s *topicSub) DeleteQueue(string) error           { return nil }
func (s *topicSub) ValidateConnection() error          { return nil }
func (s *topicSub) GetQueueLength(string) (int, error) { return 0, nil }

type countedAck struct{ acks int }

func (a *countedAck) Ack() error         { a.acks++; return nil }
func (a *countedAck) Nack(bool) error    { return nil }
func (a *countedAck) DeliveryCount() int { return 1 }

func TestTheSetupConsumerRunsTheKickedWorkspaceSetup(t *testing.T) {
	h := newHarness(t)
	if _, err := h.m.Setup.Provision(context.Background(), wsID, ""); err != nil {
		t.Fatal(err)
	}
	sub := &topicSub{handlers: map[string]func([]byte, messaging.MessageAck){}}
	consumer, err := h.m.NewSetupConsumer(sub)
	if err != nil {
		t.Fatal(err)
	}
	if err := consumer.Start(); err != nil {
		t.Fatal(err)
	}
	handle, ok := sub.handlers[es.SetupFireTopic]
	if !ok {
		t.Fatalf("subscribed topics = %v", sub.handlers)
	}
	payload, err := json.Marshal(delayedfire.Message{ID: wsID})
	if err != nil {
		t.Fatal(err)
	}
	ack := &countedAck{}
	handle(payload, ack)
	d, err := h.m.c().domainOfKind(context.Background(), wsID, es.DomainSharedProbation)
	if err != nil {
		t.Fatal(err)
	}
	if d.Status != es.DomainVerifying || ack.acks != 1 {
		t.Fatalf("domain = %+v acks = %d", d, ack.acks)
	}
	if got := h.provider.lanesOf("ensure identity"); len(got) != 1 || got[0] != es.LaneAccount {
		t.Fatalf("lanes = %v", got)
	}
}

func TestTheSetupConsumerNeedsASubscriber(t *testing.T) {
	h := newHarness(t)
	if _, err := h.m.NewSetupConsumer(nil); err == nil {
		t.Fatal("expected error")
	}
}
