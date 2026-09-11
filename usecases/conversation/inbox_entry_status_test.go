package conversation_usecase

import (
	"testing"

	"vozko/domain/conversation"
	"vozko/domain/shared"
)

// The inbox row must carry the conversation's status on EVERY channel, and this
// is a test about a regression that reached production.
//
// The status was resolved only from the official WhatsApp entry repository, so
// on unofficial WhatsApp, Instagram and Telegram the row was built with the
// zero value. The inbox renders that as "Nova". The database said ongoing.
//
// What an operator saw: they replied, the entry rebuilt, and the conversation
// moved BACKWARDS to Nova. It reads as the status transition being broken, when
// what is broken is the read — the transition had written ongoing correctly
// months earlier.
//
// This is the same shape as the AutomationEnabled bug one field over, whose
// comment in domain/conversation/repository.go already records it: a fact the
// channel union projects for every channel, consumed for only one of them.

type stubEntryLastMessage struct {
	conversation.MessageRepository
	entry conversation.EntryWithLastMessage
}

func (m stubEntryLastMessage) GetEntryLastMessage(string, shared.EntryType) (*conversation.EntryWithLastMessage, error) {
	return &m.entry, nil
}

func TestInboxEntryCarriesConversationStatusOnEveryChannel(t *testing.T) {
	// Every channel that is NOT official WhatsApp, which is the one the old
	// branch happened to cover.
	for _, entryType := range []shared.EntryType{
		shared.EntryTypeUnofficialWhatsApp,
		shared.EntryTypeInstagram,
		shared.EntryTypeTelegram,
	} {
		t.Run(string(entryType), func(t *testing.T) {
			svc := &HistoryProviderService{
				messageRepo: stubEntryLastMessage{entry: conversation.EntryWithLastMessage{
					EntryID: "entry-1", EntryType: entryType,
					LastMessageType:    conversation.MessageTypeOperator,
					ConversationStatus: string(conversation.ConversationStatusOngoing),
				}},
			}

			entry, err := svc.GetInboxEntry("entry-1", string(entryType))
			if err != nil {
				t.Fatalf("GetInboxEntry: %v", err)
			}
			if entry == nil {
				t.Fatal("no entry returned")
			}
			if entry.ConversationStatus != conversation.ConversationStatusOngoing {
				t.Errorf("conversation status = %q, want ongoing: a rebuilt row that drops it flips the conversation back to Nova on screen",
					entry.ConversationStatus)
			}
		})
	}
}

// A finished conversation must survive the rebuild too, or the row reopens
// itself on screen every time a message lands.
func TestInboxEntryKeepsAFinishedStatusOnEveryChannel(t *testing.T) {
	svc := &HistoryProviderService{
		messageRepo: stubEntryLastMessage{entry: conversation.EntryWithLastMessage{
			EntryID: "entry-1", EntryType: shared.EntryTypeTelegram,
			LastMessageType:    conversation.MessageTypeOperator,
			ConversationStatus: string(conversation.ConversationStatusFinished),
		}},
	}
	entry, err := svc.GetInboxEntry("entry-1", string(shared.EntryTypeTelegram))
	if err != nil {
		t.Fatalf("GetInboxEntry: %v", err)
	}
	if entry.ConversationStatus != conversation.ConversationStatusFinished {
		t.Errorf("conversation status = %q, want finished", entry.ConversationStatus)
	}
}

// A channel that stores no status is not a bug, and must not be reported as a
// status of "". The row simply carries nothing and the UI falls back as before.
func TestInboxEntryToleratesAChannelWithoutStatus(t *testing.T) {
	svc := &HistoryProviderService{
		messageRepo: stubEntryLastMessage{entry: conversation.EntryWithLastMessage{
			EntryID: "entry-1", EntryType: shared.EntryTypeTelegram,
			LastMessageType: conversation.MessageTypeOperator,
		}},
	}
	entry, err := svc.GetInboxEntry("entry-1", string(shared.EntryTypeTelegram))
	if err != nil {
		t.Fatalf("GetInboxEntry: %v", err)
	}
	if entry.ConversationStatus != "" {
		t.Errorf("conversation status = %q, want empty", entry.ConversationStatus)
	}
}
