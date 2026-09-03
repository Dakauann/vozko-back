package unofficial_whatsapp

import (
	"testing"

	uw "vozko/domain/unofficial_whatsapp"
)

// Whose name is on an outbound message.
//
// The provider fills `senderName` with the AUTHOR of the message, and on a
// message the operator sent that author is the connected account, not the
// person being written to. Seeding a brand-new contact from it named the
// customer after the business — and because bridgeLead copies the contact's
// DisplayName() into the CRM lead once and never revisits it, the wrong name
// outlived the contact row that was repaired seconds later by the chats sync.
//
// Live traffic: a workspace reconnected its number, the sync replayed a chat
// whose last message the operator had sent, and the customer "Dakauann" was
// filed as "Lucas - Suporte PAJ" — the operator's own WhatsApp name. 256
// contacts across 15 instances carried their own account owner's name.

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

// The inbound case is the whole point of the field and must keep working: a
// first message from an unknown number is often the only name we ever get.
func TestSubjectSeedNameKeepsInboundSenderName(t *testing.T) {
	ev := &uw.Event{Kind: uw.EventInboundMessage, SenderName: "Dakauann"}

	if ev.Outbound() {
		t.Fatal("an inbound message must not classify as outbound")
	}
	if got := subjectSeedName(ev); got != "Dakauann" {
		t.Errorf("seed name = %q, want the contact's own name", got)
	}
}

// A group's subject is the group, so the participant who spoke never names it —
// pinned here because the outbound guard sits next to this one and a rewrite
// that drops it would rename groups after their most recent talker.
func TestSubjectSeedNameNeverNamesAGroup(t *testing.T) {
	ev := &uw.Event{Kind: uw.EventInboundMessage, SenderName: "Dakauann", IsGroup: true}

	if got := subjectSeedName(ev); got != "" {
		t.Errorf("seed name = %q, a group is not named after whoever spoke", got)
	}
}
