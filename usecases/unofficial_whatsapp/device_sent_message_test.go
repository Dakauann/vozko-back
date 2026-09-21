package unofficial_whatsapp

import (
	"testing"
	"time"

	"vozko/domain/conversation"
	uw "vozko/domain/unofficial_whatsapp"
)

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

func TestDeviceSentMessageDoesNotTriggerAutomation(t *testing.T) {
	h := newGroupHarness(t, false).withFreshGate()
	h.instance.EnableAgentResponses = true

	h.deliver(t, deviceMessage(customerChat, "ja respondi"))

	if len(h.messaging.texts) != 0 {
		t.Errorf("the agent replied to the owner's own message: %v", h.messaging.texts)
	}
}

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
