package conversation_usecase

import (
	"context"
	"log"
	"testing"
	"time"

	conversation_domain "vozko/domain/conversation"
)

func signalledCall() (*whatsappCRMCall, chan conversation_domain.WhatsAppCallSignal) {
	ctx, cancel := context.WithCancel(context.Background())
	call := &whatsappCRMCall{
		id:     "wa-out-1",
		events: make(chan conversation_domain.CallEvent, 8),
		ctx:    ctx,
		cancel: cancel,
		log:    log.Default(),
	}
	signals := make(chan conversation_domain.WhatsAppCallSignal, 4)
	go call.runSignalLoop(signals)
	return call, signals
}

func nextCallEvent(t *testing.T, call *whatsappCRMCall) conversation_domain.CallEvent {
	t.Helper()
	select {
	case event := <-call.events:
		return event
	case <-time.After(time.Second):
		t.Fatal("no call event")
		return conversation_domain.CallEvent{}
	}
}

func TestTheContactsPhoneRingingIsReportedAsAlerting(t *testing.T) {
	call, signals := signalledCall()
	signals <- conversation_domain.WhatsAppCallSignal{Kind: conversation_domain.WhatsAppCallRinging}
	if event := nextCallEvent(t, call); event.Type != conversation_domain.CallEventAlerting {
		t.Fatalf("event = %+v, want alerting", event)
	}
	signals <- conversation_domain.WhatsAppCallSignal{Kind: conversation_domain.WhatsAppCallAccepted}
	if event := nextCallEvent(t, call); event.Type != conversation_domain.CallEventAnswered {
		t.Fatalf("event = %+v, want answered", event)
	}
	call.cancel()
	if event := nextCallEvent(t, call); event.Type != conversation_domain.CallEventEnded {
		t.Fatalf("event = %+v, want ended", event)
	}
}

func TestARejectedWhatsAppCallIsDeclined(t *testing.T) {
	call, signals := signalledCall()
	signals <- conversation_domain.WhatsAppCallSignal{Kind: conversation_domain.WhatsAppCallRejected, Reason: "rejected"}
	if event := nextCallEvent(t, call); event.Type != conversation_domain.CallEventDeclined {
		t.Fatalf("event = %+v, want declined", event)
	}
}
