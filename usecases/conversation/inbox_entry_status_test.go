package conversation_usecase

import (
	"testing"

	"vozko/domain/conversation"
	"vozko/domain/shared"
)

type stubEntryLastMessage struct {
	conversation.MessageRepository
	entry conversation.EntryWithLastMessage
}

func (m stubEntryLastMessage) GetEntryLastMessage(string, shared.EntryType) (*conversation.EntryWithLastMessage, error) {
	return &m.entry, nil
}

func TestInboxEntryCarriesConversationStatusOnEveryChannel(t *testing.T) {
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
