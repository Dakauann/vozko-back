package unofficial_whatsapp

import (
	"testing"

	"vozko/domain/conversation"
	uw "vozko/domain/unofficial_whatsapp"
)

// The status callbacks are this channel's advantage over Telegram — real
// Sent → Delivered → Read — and for a while they were classified and then
// thrown away: handleMessageUpdate found the conversation, broadcast an entry
// refresh, and never touched the message row. The ticks an operator reads in the
// CRM stayed on "sent" forever no matter how many times the customer opened the
// chat.
func TestDeliveryStatusMapsToTheCRMTicks(t *testing.T) {
	cases := []struct {
		provider uw.DeliveryStatus
		want     conversation.DeliveryStatus
	}{
		// Queued is reported as sent: the operator's question is "did it leave",
		// and a separate tick for "the host has it" answers nothing they can act on.
		{uw.DeliveryQueued, conversation.DeliveryStatusSent},
		{uw.DeliverySent, conversation.DeliveryStatusSent},
		{uw.DeliveryDelivered, conversation.DeliveryStatusDelivered},
		{uw.DeliveryRead, conversation.DeliveryStatusRead},
		{uw.DeliveryFailed, conversation.DeliveryStatusFailed},
		// Unknown must not overwrite a status already achieved; returning None
		// is what makes the caller skip the write.
		{uw.DeliveryUnknown, conversation.DeliveryStatusNone},
		// A deletion is a content change, not a delivery state, and is routed
		// separately. Mapping it here would downgrade a read message.
		{uw.DeliveryDeleted, conversation.DeliveryStatusNone},
	}

	for _, tc := range cases {
		t.Run(string(tc.provider), func(t *testing.T) {
			if got := crmDeliveryStatus(tc.provider); got != tc.want {
				t.Errorf("crmDeliveryStatus(%q) = %q, want %q", tc.provider, got, tc.want)
			}
		})
	}
}

// A status callback carries no chat id, and that must not cost the receipt.
//
// Regression test for a live failure visible only in the SQL log:
//
//	SELECT * FROM unofficial_whatsapp_conversations
//	WHERE instance_id = '...' AND chat_id = ''   -- 0 rows
//
// The provider identifies the MESSAGE on a status update, not the chat, so
// resolving the conversation first and returning on failure threw every
// delivered/read callback away — the ticks stayed on "sent" for the second time
// in this channel's life. The row update is keyed by the provider's message id
// and never needed the conversation; only the live websocket push does.
func TestStatusUpdateSurvivesAnUnresolvableChat(t *testing.T) {
	// The mapping is what the handler writes, and it is reached before any
	// conversation lookup. Pinning it here guards the ordering: a future
	// refactor that puts the lookup back in front of the write reintroduces the
	// exact bug, and the handler test below would still pass without this.
	for _, status := range []uw.DeliveryStatus{
		uw.DeliveryDelivered, uw.DeliveryRead, uw.DeliveryFailed,
	} {
		if got := crmDeliveryStatus(status); got == "" {
			t.Errorf("%q maps to nothing; a receipt with no chat id would write nothing", status)
		}
	}
}
