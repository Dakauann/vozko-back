package instagram

import (
	"context"
	"testing"

	"vozko/domain/conversation"
)

type recordedHistory struct {
	records []conversation.MessageHistoryRecord
}

func (h *recordedHistory) Record(_ context.Context, record conversation.MessageHistoryRecord) error {
	h.records = append(h.records, record)
	return nil
}

func TestSendPrivateReply_IsRecordedAsSentByThePerson(t *testing.T) {
	account := connectedAccount()
	history := &recordedHistory{}
	uc := NewSendPrivateReplyUseCase(
		accountRepoFor(account), &fakeMessagingService{},
		&fakeCommentRepo{}, &fakePrivateReplyRepo{},
		&fakeContactRepo{}, &fakeConversationRepo{},
	)
	uc.SetHistoryManager(history)

	if err := uc.Execute(context.Background(), account.WorkspaceID, account.ID, "comment-1", conversation.SentByPerson("user-7"), "hello"); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if len(history.records) != 1 {
		t.Fatalf("recorded %d message(s), want 1", len(history.records))
	}
	if got := history.records[0].SentBy; got != conversation.SentByPerson("user-7") {
		t.Fatalf("sender %+v, want the person who replied", got)
	}
}
