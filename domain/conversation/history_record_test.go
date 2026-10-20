package conversation

import (
	"encoding/json"
	"testing"
	"time"

	"vozko/domain/shared"
)

func TestARecordBecomesTheMessageTheConversationStores(t *testing.T) {
	at := time.Date(2026, 10, 20, 9, 30, 0, 0, time.UTC)
	cases := []struct {
		name   string
		record MessageHistoryRecord
		check  func(t *testing.T, m *Message)
	}{
		{
			name: "a workflow text keeps its author, channel and provider id",
			record: MessageHistoryRecord{
				SentBy: SentByWorkflow("wf-1"), EntryID: " e1 ", EntryType: shared.EntryTypeWhatsApp, Channel: MessageChannelWhatsApp,
				MessageType: MessageTypeAIResponse, MessageID: "wamid.1", From: "551100", To: "551199", Text: "Olá", Timestamp: at,
			},
			check: func(t *testing.T, m *Message) {
				if m.ID != "" || m.EntryID != "e1" || m.MessageType != MessageTypeAIResponse || m.Text != "Olá" || !m.CreatedAt.Equal(at) {
					t.Fatalf("message = %+v", m)
				}
				if m.WhatsAppMessageID == nil || *m.WhatsAppMessageID != "wamid.1" || m.ExternalMessageID != nil {
					t.Fatalf("provider ids = %v %v", m.WhatsAppMessageID, m.ExternalMessageID)
				}
				if m.Direction != MessageDirectionOutbound || m.SentBy != SentByWorkflow("wf-1") {
					t.Fatalf("author = %v %v", m.Direction, m.SentBy)
				}
			},
		},
		{
			name:   "a provider id wins over the whatsapp id",
			record: MessageHistoryRecord{EntryID: "e1", MessageID: "wamid.1", ProviderMessageID: "ig-9", Text: "x"},
			check: func(t *testing.T, m *Message) {
				if m.ExternalMessageID == nil || *m.ExternalMessageID != "ig-9" || m.WhatsAppMessageID != nil {
					t.Fatalf("provider ids = %v %v", m.WhatsAppMessageID, m.ExternalMessageID)
				}
			},
		},
		{
			name:   "unknown kinds fall back to whatsapp and to an outbound reply",
			record: MessageHistoryRecord{EntryID: "e1", EntryType: "nope", Channel: "nope", MessageType: "nope", Text: "x"},
			check: func(t *testing.T, m *Message) {
				if m.EntryType != shared.EntryTypeWhatsApp || m.Channel != MessageChannelWhatsApp || m.MessageType != MessageTypeAIResponse {
					t.Fatalf("message = %+v", m)
				}
				if m.CreatedAt.IsZero() || !m.CreatedAt.Equal(m.UpdatedAt) {
					t.Fatalf("a record without a time is stamped now, got %v", m.CreatedAt)
				}
			},
		},
		{
			name:   "a contact record without a kind is a user message",
			record: MessageHistoryRecord{EntryID: "e1", SentBy: SentByContact("5511"), Text: "oi"},
			check: func(t *testing.T, m *Message) {
				if m.MessageType != MessageTypeUserMessage {
					t.Fatalf("type = %s", m.MessageType)
				}
			},
		},
		{
			name:   "media is attached only through its stored id",
			record: MessageHistoryRecord{EntryID: "e1", MessageType: MessageTypeMedia, MediaType: MediaTypeImage, MediaURL: "https://cdn/x.jpg"},
			check: func(t *testing.T, m *Message) {
				if m.MediaID != nil || m.MediaType != "" {
					t.Fatalf("media = %v %q", m.MediaID, m.MediaType)
				}
			},
		},
		{
			name:   "a stored media id carries its kind",
			record: MessageHistoryRecord{EntryID: "e1", MediaID: "cm-1", MediaType: MediaTypeVideo},
			check: func(t *testing.T, m *Message) {
				if m.MediaID == nil || *m.MediaID != "cm-1" || m.MediaType != MediaTypeVideo {
					t.Fatalf("media = %v %q", m.MediaID, m.MediaType)
				}
			},
		},
		{
			name:   "a read record is read now with its metadata kept",
			record: MessageHistoryRecord{EntryID: "e1", Text: "x", Read: true, Metadata: json.RawMessage(`{"seed":"lead_import"}`)},
			check: func(t *testing.T, m *Message) {
				if !m.Read || m.ReadAt == nil || string(m.Metadata) != `{"seed":"lead_import"}` {
					t.Fatalf("message = %+v", m)
				}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.check(t, tc.record.Message())
		})
	}
}
