package unofficial_whatsapp

import (
	"testing"
	"time"

	"vozko/domain/conversation"
	uw "vozko/domain/unofficial_whatsapp"
)

// A message the owner types on their OWN WhatsApp app.
//
// This is the only channel where it can happen — a linked device is a real
// phone somebody still uses — and it is the case that broke the transcript. The
// provider's schema is explicit that `fromMe` says who sent a message and
// `messageType` says what it contained, so a text the owner typed arrives as an
// ordinary text. Reading direction off the content type filed their reply on the
// CUSTOMER's side: drawn left-aligned, labelled with the customer's own name and
// picture, and leaving the conversation in NEW as though nobody had answered.
//
// The fix is to carry the direction the provider already told us, so these tests
// are about that fact surviving the whole ingest path rather than about the
// message body.

// deviceMessage is what the provider sends when the owner replies from their
// phone: fromMe, and no track id because we did not send it.
func deviceMessage(chatID, text string) map[string]any {
	return map[string]any{
		"messageid":        "dev-" + text,
		"chatid":           chatID,
		"sender":           "5599999999999@s.whatsapp.net",
		"fromMe":           true,
		"messageType":      "text",
		"text":             text,
		"messageTimestamp": time.Now().UnixMilli(),
	}
}

// customerMessage is the same conversation, the other way.
func customerMessage(chatID, text string) map[string]any {
	return map[string]any{
		"messageid":        "in-" + text,
		"chatid":           chatID,
		"sender":           chatID,
		"sender_pn":        chatID,
		"fromMe":           false,
		"messageType":      "text",
		"text":             text,
		"messageTimestamp": time.Now().UnixMilli(),
	}
}

const customerChat = "5511999999999@s.whatsapp.net"

// The bug: the owner's own reply must be recorded as OUTBOUND.
func TestOwnerReplyFromPhoneIsRecordedOutbound(t *testing.T) {
	h := newGroupHarness(t, false).withFreshGate()

	h.deliver(t, customerMessage(customerChat, "bom dia"))
	h.deliver(t, deviceMessage(customerChat, "bom dia, ja verifico"))

	records := h.history.all()
	if len(records) != 2 {
		t.Fatalf("recorded %d messages, want 2", len(records))
	}

	if got := h.history.directionOf(0); got != conversation.MessageDirectionInbound {
		t.Errorf("the customer's message was recorded %q, want INBOUND", got)
	}
	if got := h.history.directionOf(1); got != conversation.MessageDirectionOutbound {
		t.Fatalf("the owner's own reply was recorded %q, want OUTBOUND — "+
			"it will render as the customer's message", got)
	}
}

// The content type stays honest. This is the half a channel gives up when it
// encodes direction in the type instead: Telegram and Instagram store every
// outbound message as `operator`, so a photo sent from the app reads back as a
// plain note. Both facts are kept here.
func TestDeviceSentMessageKeepsItsContentType(t *testing.T) {
	h := newGroupHarness(t, false).withFreshGate()

	h.deliver(t, deviceMessage(customerChat, "segue"))

	records := h.history.all()
	if len(records) != 1 {
		t.Fatalf("recorded %d messages, want 1", len(records))
	}
	if got := records[0].MessageType; got != conversation.MessageTypeUserMessage {
		t.Errorf("message type = %q; the content type was overwritten to encode direction", got)
	}
}

// From/To are the number and the contact, the right way round. The renderer's
// legacy fallback compares `to` against the subject, so a reversed pair would
// place the message correctly by direction and wrongly by every older reading.
func TestDeviceSentMessageIsAddressedToTheContact(t *testing.T) {
	h := newGroupHarness(t, false).withFreshGate()

	h.deliver(t, deviceMessage(customerChat, "ok"))

	records := h.history.all()
	if len(records) != 1 {
		t.Fatalf("recorded %d messages, want 1", len(records))
	}
	if records[0].From != h.instance.Label() {
		t.Errorf("from = %q, want the number's own label %q", records[0].From, h.instance.Label())
	}
	if records[0].To != "+5511999999999" {
		t.Errorf("to = %q, want the contact", records[0].To)
	}
}

// The owner answering on their phone must not wake the agent. It would be
// answering a colleague, and on a channel where both sides share one number that
// is a loop with no natural end.
func TestDeviceSentMessageDoesNotTriggerAutomation(t *testing.T) {
	h := newGroupHarness(t, false).withFreshGate()
	h.instance.EnableAgentResponses = true

	h.deliver(t, deviceMessage(customerChat, "ja respondi"))

	if len(h.messaging.texts) != 0 {
		t.Errorf("the agent replied to the owner's own message: %v", h.messaging.texts)
	}
}

// A group behaves the same way. Worth its own case because a group message
// carries a participant sender, and the owner speaking in a group is the one
// place where fromMe and a group participant appear together.
func TestOwnerReplyInAGroupIsRecordedOutbound(t *testing.T) {
	h := newGroupHarness(t, true).withFreshGate()
	const groupChat = "120363012345678901@g.us"

	h.deliver(t, groupMessage("5511111111111", "alguem viu isso?"))
	h.deliver(t, deviceMessage(groupChat, "estou vendo agora"))

	if got := h.history.directionOf(0); got != conversation.MessageDirectionInbound {
		t.Errorf("a member's message was recorded %q, want INBOUND", got)
	}
	if got := h.history.directionOf(1); got != conversation.MessageDirectionOutbound {
		t.Errorf("the owner's reply in the group was recorded %q, want OUTBOUND", got)
	}
}

// Our own send coming back as an echo is outbound too, and must not be inserted
// twice — the history manager dedups on the provider id, and the direction it
// carries has to agree with the row already there.
func TestOurOwnEchoIsOutbound(t *testing.T) {
	h := newGroupHarness(t, false).withFreshGate()

	echo := deviceMessage(customerChat, "enviado pelo crm")
	echo["wasSentByApi"] = true
	echo["track_source"] = uw.TrackSource
	echo["track_id"] = "track-123"
	h.deliver(t, echo)

	if got := h.history.directionOf(0); got != conversation.MessageDirectionOutbound {
		t.Errorf("our own echo was recorded %q, want OUTBOUND", got)
	}
}
