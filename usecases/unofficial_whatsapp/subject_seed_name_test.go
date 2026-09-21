package unofficial_whatsapp

import (
	"testing"

	uw "vozko/domain/unofficial_whatsapp"
)

func TestSubjectSeedNameIgnoresOutboundSenderName(t *testing.T) {
	const ownerName = "Lucas - Suporte PAJ"

	for _, tc := range []struct {
		name string
		kind uw.EventKind
	}{
		{"our own send echoed back", uw.EventOutboundEcho},
		{"owner typed it on their phone", uw.EventOutboundFromDevice},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ev := &uw.Event{Kind: tc.kind, SenderName: ownerName}
			if !ev.Outbound() {
				t.Fatalf("%s must classify as outbound, the guard hangs off it", tc.kind)
			}
			if got := subjectSeedName(ev); got != "" {
				t.Errorf("seed name = %q, want empty: %q is the connected account, not the contact", got, ownerName)
			}
		})
	}
}

func TestSubjectSeedNameKeepsInboundSenderName(t *testing.T) {
	ev := &uw.Event{Kind: uw.EventInboundMessage, SenderName: "Dakauann"}

	if ev.Outbound() {
		t.Fatal("an inbound message must not classify as outbound")
	}
	if got := subjectSeedName(ev); got != "Dakauann" {
		t.Errorf("seed name = %q, want the contact's own name", got)
	}
}

func TestSubjectSeedNameNeverNamesAGroup(t *testing.T) {
	ev := &uw.Event{Kind: uw.EventInboundMessage, SenderName: "Dakauann", IsGroup: true}

	if got := subjectSeedName(ev); got != "" {
		t.Errorf("seed name = %q, a group is not named after whoever spoke", got)
	}
}
