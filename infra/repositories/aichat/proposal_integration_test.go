package aichat_repository

import (
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"vozko/domain/aichat"
	"vozko/infra/database/schema"
	"vozko/infra/repositories/repotest"
)

func integrationDB(t *testing.T) *gorm.DB {
	return repotest.IsolatedDB(t, "chat_test", &schema.AIChatMessage{})
}

func proposed(t *testing.T, repo aichat.MessageRepository, threadID, proposalID string) {
	t.Helper()
	m := &aichat.Message{ThreadID: threadID, Role: aichat.RoleAssistant, Content: "Proponho",
		ProposalID: proposalID, Proposal: []byte(`{"id":"` + proposalID + `","toolName":"create_label"}`), ProposalStatus: aichat.ProposalPending}
	if err := repo.Create(m); err != nil {
		t.Fatalf("create: %v", err)
	}
}

func TestOnlyOneConcurrentClaimWinsAProposal(t *testing.T) {
	repo := NewMessageRepository(integrationDB(t))
	thread := uuid.New().String()
	proposed(t, repo, thread, "act-1")

	var wins, losses int
	var mu sync.Mutex
	var wg sync.WaitGroup
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m, err := repo.ClaimProposal(thread, "act-1", aichat.ProposalApproved)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil && m.ProposalID == "act-1" && string(m.Proposal) != "":
				wins++
			case errors.Is(err, aichat.ErrProposalNotPending):
				losses++
			default:
				t.Errorf("claim: %+v %v", m, err)
			}
		}()
	}
	wg.Wait()
	if wins != 1 || losses != 9 {
		t.Fatalf("wins %d losses %d, want exactly one winner", wins, losses)
	}
}

func TestClaimIsScopedToTheThreadAndExpiryStopsIt(t *testing.T) {
	repo := NewMessageRepository(integrationDB(t))
	thread, other := uuid.New().String(), uuid.New().String()
	proposed(t, repo, thread, "act-1")
	if _, err := repo.ClaimProposal(other, "act-1", aichat.ProposalApproved); !errors.Is(err, aichat.ErrProposalNotPending) {
		t.Fatalf("another thread claimed it: %v", err)
	}
	if err := repo.ExpireProposals(thread); err != nil {
		t.Fatalf("expire: %v", err)
	}
	if _, err := repo.ClaimProposal(thread, "act-1", aichat.ProposalApproved); !errors.Is(err, aichat.ErrProposalNotPending) {
		t.Fatalf("an expired proposal was claimed: %v", err)
	}
	items, _, err := repo.ListByThread(aichat.ListMessagesInput{ThreadID: thread, Limit: 10})
	if err != nil || len(items) != 1 || items[0].ProposalStatus != aichat.ProposalExpired || items[0].ProposalID != "act-1" {
		t.Fatalf("listed %+v err %v", items, err)
	}
}
