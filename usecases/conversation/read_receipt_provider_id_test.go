package conversation_usecase

import (
	"testing"

	"vozko/domain/conversation"
	"vozko/domain/shared"
)

// The receipt an operator sends by opening a conversation is resolved from ONE
// message id — the newest inbound one. Two things made that resolution return
// nothing, or the wrong thing, on every channel except official WhatsApp:
//
//   - only whatsapp_message_id was read, and no adapter-backed channel fills it
//     (they use external_message_id), so the lookup returned "" and the receipt
//     was silently skipped: the operator read the chat, the contact's ticks
//     stayed grey;
//   - the INBOUND test used the message TYPE, but on unofficial WhatsApp a
//     message we sent is user_message too, so our own outbound id could be
//     handed to the provider to mark as read.

type stubMessageReader struct {
	conversation.MessageRepository
	byID map[string]*conversation.Message
}

func (r *stubMessageReader) GetByID(id string) (*conversation.Message, error) {
	if m, ok := r.byID[id]; ok {
		return m, nil
	}
	return nil, conversation.ErrMessageNotFound
}

func strPtr(s string) *string { return &s }

const testEntryID = "entry-1"

func markerWith(msgs ...*conversation.Message) (*MessageMarkerService, []string) {
	repo := &stubMessageReader{byID: map[string]*conversation.Message{}}
	ids := make([]string, 0, len(msgs))
	for _, m := range msgs {
		if m.EntryID == "" {
			m.EntryID = testEntryID
		}
		repo.byID[m.ID] = m
		ids = append(ids, m.ID)
	}
	return &MessageMarkerService{messageRepo: repo}, ids
}

func TestReadReceiptResolvesAdapterChannelProviderID(t *testing.T) {
	marker, ids := markerWith(
		&conversation.Message{
			ID: "m1", EntryType: shared.EntryTypeUnofficialWhatsApp,
			Direction: conversation.MessageDirectionInbound,
			// The type a contact's text carries on this channel — and the type
			// OUR text carries too, which is why direction has to decide.
			MessageType:       conversation.MessageTypeUserMessage,
			ExternalMessageID: strPtr("3EB027B8F1853217E3B8BB"),
		},
	)

	if got := marker.latestInboundProviderID(testEntryID, shared.EntryTypeUnofficialWhatsApp, ids); got != "3EB027B8F1853217E3B8BB" {
		t.Errorf("provider id = %q, want the external id — an empty result skips the receipt entirely", got)
	}
}

// Direction is the honest signal; the type is not. Offering an outbound id here
// asks the provider to mark OUR OWN message as read.
func TestReadReceiptIgnoresOurOwnOutboundMessages(t *testing.T) {
	marker, ids := markerWith(
		&conversation.Message{
			ID: "m1", EntryType: shared.EntryTypeUnofficialWhatsApp,
			Direction:         conversation.MessageDirectionInbound,
			MessageType:       conversation.MessageTypeUserMessage,
			ExternalMessageID: strPtr("INBOUND-1"),
		},
		&conversation.Message{
			ID: "m2", EntryType: shared.EntryTypeUnofficialWhatsApp,
			Direction:         conversation.MessageDirectionOutbound,
			MessageType:       conversation.MessageTypeUserMessage, // same type, ours
			ExternalMessageID: strPtr("OUTBOUND-1"),
		},
	)

	// Newest-first: m2 is newest but ours, so the newest INBOUND must win.
	if got := marker.latestInboundProviderID(testEntryID, shared.EntryTypeUnofficialWhatsApp, ids); got != "INBOUND-1" {
		t.Errorf("provider id = %q, want INBOUND-1", got)
	}
}

// Official WhatsApp is the path that already worked and must keep working.
func TestReadReceiptStillResolvesOfficialWhatsAppWamid(t *testing.T) {
	marker, ids := markerWith(
		&conversation.Message{
			ID: "m1", EntryType: shared.EntryTypeWhatsApp,
			Direction:         conversation.MessageDirectionInbound,
			MessageType:       conversation.MessageTypeUserMessage,
			WhatsAppMessageID: strPtr("wamid.HBgMNTU="),
		},
	)

	if got := marker.latestInboundProviderID(testEntryID, shared.EntryTypeWhatsApp, ids); got != "wamid.HBgMNTU=" {
		t.Errorf("provider id = %q, want the wamid", got)
	}
}

// Rows written before direction was persisted carry only a type, so the type
// still has to work as a fallback rather than being ignored outright.
func TestReadReceiptFallsBackToMessageTypeWhenDirectionIsUnset(t *testing.T) {
	marker, ids := markerWith(
		&conversation.Message{
			ID: "m1", EntryType: shared.EntryTypeWhatsApp,
			MessageType:       conversation.MessageTypeUserMessage, // inbound by type
			WhatsAppMessageID: strPtr("legacy-inbound"),
		},
		&conversation.Message{
			ID: "m2", EntryType: shared.EntryTypeWhatsApp,
			MessageType:       conversation.MessageTypeAIResponse, // outbound by type
			WhatsAppMessageID: strPtr("legacy-outbound"),
		},
	)

	if got := marker.latestInboundProviderID(testEntryID, shared.EntryTypeWhatsApp, ids); got != "legacy-inbound" {
		t.Errorf("provider id = %q, want legacy-inbound", got)
	}
}

// Nothing inbound to acknowledge is a normal state, not a receipt for whatever
// id happens to be lying around.
func TestReadReceiptReturnsNothingWhenNoInboundIDExists(t *testing.T) {
	marker, ids := markerWith(
		&conversation.Message{
			ID: "m1", EntryType: shared.EntryTypeUnofficialWhatsApp,
			Direction:         conversation.MessageDirectionOutbound,
			MessageType:       conversation.MessageTypeUserMessage,
			ExternalMessageID: strPtr("OUTBOUND-ONLY"),
		},
	)

	if got := marker.latestInboundProviderID(testEntryID, shared.EntryTypeUnofficialWhatsApp, ids); got != "" {
		t.Errorf("provider id = %q, want empty", got)
	}
}

// The message ids arrive from the client. The database write is already scoped
// to the entry, but this receipt LEAVES the platform: a foreign id must not be
// handed to the provider on this conversation's channel.
func TestReadReceiptIgnoresMessagesFromAnotherEntry(t *testing.T) {
	marker, ids := markerWith(
		&conversation.Message{
			ID: "mine", EntryID: testEntryID, EntryType: shared.EntryTypeUnofficialWhatsApp,
			Direction:         conversation.MessageDirectionInbound,
			MessageType:       conversation.MessageTypeUserMessage,
			ExternalMessageID: strPtr("MINE-1"),
		},
		&conversation.Message{
			ID: "foreign", EntryID: "someone-elses-entry", EntryType: shared.EntryTypeUnofficialWhatsApp,
			Direction:         conversation.MessageDirectionInbound,
			MessageType:       conversation.MessageTypeUserMessage,
			ExternalMessageID: strPtr("FOREIGN-1"),
		},
	)

	// The foreign one is newest, so without the check it would win.
	if got := marker.latestInboundProviderID(testEntryID, shared.EntryTypeUnofficialWhatsApp, ids); got != "MINE-1" {
		t.Errorf("provider id = %q, want MINE-1 — a foreign id must never reach the provider", got)
	}
}

// Same id, different channel: the entry type is part of a message's identity,
// so a Telegram row must not answer a receipt going out on WhatsApp.
func TestReadReceiptIgnoresAnotherChannelsMessage(t *testing.T) {
	marker, ids := markerWith(
		&conversation.Message{
			ID: "tg", EntryID: testEntryID, EntryType: shared.EntryTypeTelegram,
			Direction:         conversation.MessageDirectionInbound,
			MessageType:       conversation.MessageTypeUserMessage,
			ExternalMessageID: strPtr("TELEGRAM-1"),
		},
	)

	if got := marker.latestInboundProviderID(testEntryID, shared.EntryTypeUnofficialWhatsApp, ids); got != "" {
		t.Errorf("provider id = %q, want empty", got)
	}
}

// Official WhatsApp had its own copy of this resolution, testing the message
// TYPE. Media we SENT carries message_type "media", which IsInbound() calls
// inbound, so the receipt named our own message and Meta refused it:
// "(#100) ... is outgoing. Please use an incoming message ID."
func TestReadReceiptRejectsOutboundMediaOnOfficialWhatsApp(t *testing.T) {
	marker, ids := markerWith(
		&conversation.Message{
			ID: "m1", EntryID: testEntryID, EntryType: shared.EntryTypeWhatsApp,
			Direction:         conversation.MessageDirectionInbound,
			MessageType:       conversation.MessageTypeUserMessage,
			WhatsAppMessageID: strPtr("wamid.INBOUND"),
		},
		&conversation.Message{
			ID: "m2", EntryID: testEntryID, EntryType: shared.EntryTypeWhatsApp,
			Direction: conversation.MessageDirectionOutbound,
			// The type that fooled the old check: ours, but "inbound" by type.
			MessageType:       conversation.MessageTypeMedia,
			WhatsAppMessageID: strPtr("wamid.OUR-MEDIA"),
		},
	)

	got := marker.latestInboundProviderID(testEntryID, shared.EntryTypeWhatsApp, ids)
	if got == "wamid.OUR-MEDIA" {
		t.Fatal("our own media must never be sent as a read receipt; Meta rejects it")
	}
	if got != "wamid.INBOUND" {
		t.Errorf("provider id = %q, want wamid.INBOUND", got)
	}
}
