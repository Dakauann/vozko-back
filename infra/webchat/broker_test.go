package webchat

import (
	"context"
	"errors"
	"testing"

	wcdomain "vozko/domain/webchat"
)

type loopback struct{ handler func([]byte) }

func (l *loopback) Publish(_ string, data []byte) error {
	l.handler(data)
	return nil
}

func (l *loopback) Subscribe(_ context.Context, _ string, handler func([]byte)) { l.handler = handler }

func startedBroker() *Broker {
	b := NewBroker(&loopback{})
	b.Start(context.Background())
	return b
}

func TestEventsReachOnlyTheirVisitor(t *testing.T) {
	b := startedBroker()
	mine, cancelMine, _ := b.Subscribe("v1")
	defer cancelMine()
	theirs, cancelTheirs, _ := b.Subscribe("v2")
	defer cancelTheirs()

	if err := b.Publish(context.Background(), wcdomain.VisitorEvent{VisitorID: "v1", Kind: wcdomain.EventTyping, Typing: true}); err != nil {
		t.Fatal(err)
	}
	select {
	case ev := <-mine:
		if ev.Kind != wcdomain.EventTyping || ev.VisitorID != "v1" {
			t.Fatalf("event = %+v", ev)
		}
	default:
		t.Fatal("the visitor did not receive its event")
	}
	select {
	case ev := <-theirs:
		t.Fatalf("another visitor received %+v", ev)
	default:
	}
}

func TestStreamsPerVisitorAreCapped(t *testing.T) {
	b := startedBroker()
	for i := 0; i < MaxStreamsPerVisitor; i++ {
		if _, _, err := b.Subscribe("v1"); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := b.Subscribe("v1"); !errors.Is(err, wcdomain.ErrRateLimited) {
		t.Fatalf("extra stream = %v", err)
	}
}

func TestCancelFreesTheSlotAndIsIdempotent(t *testing.T) {
	b := startedBroker()
	_, cancel, _ := b.Subscribe("v1")
	cancel()
	cancel()
	if b.total != 0 || len(b.streams) != 0 {
		t.Fatalf("total %d streams %d", b.total, len(b.streams))
	}
}

func TestPublishWithoutAVisitorIsRefused(t *testing.T) {
	if err := startedBroker().Publish(context.Background(), wcdomain.VisitorEvent{Kind: wcdomain.EventMessage}); err == nil {
		t.Fatal("an event with no visitor must not be broadcast")
	}
}
