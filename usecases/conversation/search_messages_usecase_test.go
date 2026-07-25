package conversation_usecase

import (
	"errors"
	"testing"

	"vozko/domain/conversation"
)

type searchMessagesRepoStub struct {
	conversation.MessageRepository
	gotInput conversation.SearchMessagesByEntryInput
	msgs     []*conversation.Message
	total    int64
	err      error
}

func (s *searchMessagesRepoStub) SearchMessagesByEntry(input conversation.SearchMessagesByEntryInput) ([]*conversation.Message, int64, error) {
	s.gotInput = input
	return s.msgs, s.total, s.err
}

// The usecase must forward the exact input and return the repo's results
// verbatim — identical to the handler's previous direct repo call.
func TestSearchMessagesUseCaseDelegatesToRepo(t *testing.T) {
	repo := &searchMessagesRepoStub{
		msgs:  []*conversation.Message{{ID: "m1"}},
		total: 1,
	}
	uc := NewSearchMessagesByEntryUseCase(repo)

	input := conversation.SearchMessagesByEntryInput{EntryID: "e1", Query: "hello", Page: 1, PageSize: 20}
	msgs, total, err := uc.Execute(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repo.gotInput != input {
		t.Errorf("repo got input %+v, want %+v", repo.gotInput, input)
	}
	if total != 1 || len(msgs) != 1 || msgs[0].ID != "m1" {
		t.Errorf("unexpected results: msgs=%v total=%d", msgs, total)
	}
}

func TestSearchMessagesUseCasePropagatesError(t *testing.T) {
	sentinel := errors.New("boom")
	uc := NewSearchMessagesByEntryUseCase(&searchMessagesRepoStub{err: sentinel})
	if _, _, err := uc.Execute(conversation.SearchMessagesByEntryInput{}); !errors.Is(err, sentinel) {
		t.Fatalf("expected error to propagate, got %v", err)
	}
}
