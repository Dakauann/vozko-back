package inbox_assignment_usecase

import (
	"errors"
	"testing"

	ia "vozko/domain/inbox_assignment"
)

type ownerRepoStub struct {
	ia.Repository
	assignment *ia.InboxAssignment
	err        error
}

func (s ownerRepoStub) FindByEntry(string, string, string) (*ia.InboxAssignment, error) {
	return s.assignment, s.err
}

func TestConversationOwnerIsWhoeverHoldsTheConversation(t *testing.T) {
	for want, assignment := range map[string]*ia.InboxAssignment{
		"user-7":     {AssignedUserID: "user-7"},
		"ai:agent-1": {AssignedUserID: "ai:agent-1"},
		"":           nil,
	} {
		got, err := NewConversationOwners(ownerRepoStub{assignment: assignment}).ConversationOwner("ws", "e", "instagram")
		if err != nil || got != want {
			t.Errorf("owner = %q, %v, want %q", got, err, want)
		}
	}
}

func TestConversationOwnerReportsLookupFailures(t *testing.T) {
	boom := errors.New("db down")
	if _, err := NewConversationOwners(ownerRepoStub{err: boom}).ConversationOwner("ws", "e", "instagram"); !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
}
