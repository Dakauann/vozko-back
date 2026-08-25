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

func markerWith(msgs ...*conversation.Message) (*MessageMarkerService, []string) {
	repo := &stubMessageReader{byID: map[string]*conversation.Message{}}
	ids := make([]string, 0, len(msgs))
	for _, m := range msgs {
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

	if got := marker.latestInboundProviderID(ids); got != "3EB027B8F1853217E3B8BB" {
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
	if got := marker.latestInboundProviderID(ids); got != "INBOUND-1" {
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

	if got := marker.latestInboundProviderID(ids); got != "wamid.HBgMNTU=" {
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

	if got := marker.latestInboundProviderID(ids); got != "legacy-inbound" {
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

	if got := marker.latestInboundProviderID(ids); got != "" {
		t.Errorf("provider id = %q, want empty", got)
	}
}
