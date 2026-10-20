package conversation_usecase

import (
	"testing"
	"time"

	"vozko/domain/conversation"
	"vozko/domain/shared"
)

func TestInboxEntryCarriesWhenTheCustomerAndTheTeamLastWroteOnEveryChannel(t *testing.T) {
	customer := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	agent := customer.Add(-time.Hour)
	for _, entryType := range shared.ConversationViewableEntryTypes() {
		t.Run(string(entryType), func(t *testing.T) {
			svc := &HistoryProviderService{
				messageRepo: stubEntryLastMessage{entry: conversation.EntryWithLastMessage{
					EntryID: "entry-1", EntryType: entryType,
					LastMessageType:       conversation.MessageTypeUserMessage,
					LastCustomerMessageAt: &customer,
					LastAgentMessageAt:    &agent,
				}},
			}

			entry, err := svc.GetInboxEntry("entry-1", string(entryType))
			if err != nil || entry == nil {
				t.Fatalf("GetInboxEntry = %+v, %v", entry, err)
			}
			if entry.LastCustomerMessageAt == nil || !entry.LastCustomerMessageAt.Equal(customer) {
				t.Errorf("last customer message at = %v, want %v: the row would show no waiting time", entry.LastCustomerMessageAt, customer)
			}
			if entry.LastAgentMessageAt == nil || !entry.LastAgentMessageAt.Equal(agent) {
				t.Errorf("last agent message at = %v, want %v", entry.LastAgentMessageAt, agent)
			}
		})
	}
}

func TestInboxEntryWaitsSinceTheFirstUnansweredMessageOnEveryChannel(t *testing.T) {
	since := time.Date(2026, 10, 19, 9, 0, 0, 0, time.UTC)
	for _, entryType := range shared.ConversationViewableEntryTypes() {
		for _, c := range []struct {
			status conversation.ConversationStatus
			want   *time.Time
		}{
			{conversation.ConversationStatusOngoing, &since},
			{conversation.ConversationStatusNew, &since},
			{conversation.ConversationStatusFinished, nil},
		} {
			t.Run(string(entryType)+"/"+string(c.status), func(t *testing.T) {
				svc := &HistoryProviderService{
					messageRepo: stubEntryLastMessage{entry: conversation.EntryWithLastMessage{
						EntryID: "entry-1", EntryType: entryType,
						LastMessageType:    conversation.MessageTypeUserMessage,
						ConversationStatus: string(c.status),
						AwaitingReplySince: &since,
					}},
				}
				entry, err := svc.GetInboxEntry("entry-1", string(entryType))
				if err != nil || entry == nil {
					t.Fatalf("GetInboxEntry = %+v, %v", entry, err)
				}
				switch {
				case c.want == nil && entry.WaitingSince != nil:
					t.Fatalf("a %s conversation shows waiting since %v", c.status, *entry.WaitingSince)
				case c.want != nil && (entry.WaitingSince == nil || !entry.WaitingSince.Equal(*c.want)):
					t.Fatalf("waiting since = %v, want %v", entry.WaitingSince, *c.want)
				}
			})
		}
	}
}
