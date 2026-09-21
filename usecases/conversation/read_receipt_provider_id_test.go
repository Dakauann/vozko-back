package conversation_usecase

import (
	"testing"

	"vozko/domain/conversation"
	"vozko/domain/shared"
)

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
			Direction:         conversation.MessageDirectionInbound,
			MessageType:       conversation.MessageTypeUserMessage,
			ExternalMessageID: strPtr("3EB027B8F1853217E3B8BB"),
		},
	)

	if got := marker.latestInboundProviderID(testEntryID, shared.EntryTypeUnofficialWhatsApp, ids); got != "3EB027B8F1853217E3B8BB" {
		t.Errorf("provider id = %q, want the external id — an empty result skips the receipt entirely", got)
	}
}

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
			MessageType:       conversation.MessageTypeUserMessage,
			ExternalMessageID: strPtr("OUTBOUND-1"),
		},
	)

	if got := marker.latestInboundProviderID(testEntryID, shared.EntryTypeUnofficialWhatsApp, ids); got != "INBOUND-1" {
		t.Errorf("provider id = %q, want INBOUND-1", got)
	}
}

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

func TestReadReceiptFallsBackToMessageTypeWhenDirectionIsUnset(t *testing.T) {
	marker, ids := markerWith(
		&conversation.Message{
			ID: "m1", EntryType: shared.EntryTypeWhatsApp,
			MessageType:       conversation.MessageTypeUserMessage,
			WhatsAppMessageID: strPtr("legacy-inbound"),
		},
		&conversation.Message{
			ID: "m2", EntryType: shared.EntryTypeWhatsApp,
			MessageType:       conversation.MessageTypeAIResponse,
			WhatsAppMessageID: strPtr("legacy-outbound"),
		},
	)

	if got := marker.latestInboundProviderID(testEntryID, shared.EntryTypeWhatsApp, ids); got != "legacy-inbound" {
		t.Errorf("provider id = %q, want legacy-inbound", got)
	}
}

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

	if got := marker.latestInboundProviderID(testEntryID, shared.EntryTypeUnofficialWhatsApp, ids); got != "MINE-1" {
		t.Errorf("provider id = %q, want MINE-1 — a foreign id must never reach the provider", got)
	}
}

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
			Direction:         conversation.MessageDirectionOutbound,
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
