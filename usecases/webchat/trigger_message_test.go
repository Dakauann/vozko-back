package webchat

import (
	"context"
	"testing"
)

func TestTheAutomationGetsTheProviderIDTheTranscriptStored(t *testing.T) {
	h := newHarness(t, nil)
	s := h.session(t)

	msg, err := h.svc.Send(context.Background(), s, SendInput{ClientMessageID: "client-0002", Text: "oi"})

	if err != nil {
		t.Fatal(err)
	}
	if len(h.automation.inputs) != 1 || len(h.transcript.records) != 1 {
		t.Fatalf("inputs %d records %d", len(h.automation.inputs), len(h.transcript.records))
	}
	stored := h.transcript.records[0].ProviderMessageID
	if got := h.automation.inputs[0].ProviderMessageID; got == "" || got != stored || got != msg.ID {
		t.Fatalf("automation provider id = %q, stored %q, message %q", got, stored, msg.ID)
	}
}
