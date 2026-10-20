package conversation_usecase

import (
	"errors"
	"testing"

	"vozko/domain/conversation"
	"vozko/domain/shared"
)

type agentRunCancellerSpy struct {
	calls []string
	err   error
}

func (s *agentRunCancellerSpy) CancelAgentRunsOfFinishedConversation(entryID, entryType string) error {
	s.calls = append(s.calls, entryType+"/"+entryID)
	return s.err
}

func TestFinishingAConversationCancelsTheRunsItsAgentStarted(t *testing.T) {
	repo := &outcomeEntryRepo{status: string(conversation.ConversationStatusOngoing)}
	svc := outcomeServiceWith(t, repo, nil)
	runs := &agentRunCancellerSpy{}
	svc.SetAgentRunCanceller(runs)

	if err := svc.Finish("entry-1", string(shared.EntryTypeWhatsApp), conversation.FinishOptions{Source: conversation.CloseSourceHuman}); err != nil {
		t.Fatal(err)
	}
	if len(runs.calls) != 1 || runs.calls[0] != "whatsapp/entry-1" {
		t.Fatalf("cancel calls = %v", runs.calls)
	}
}

func TestOnlyAFinishCancelsAgentRuns(t *testing.T) {
	repo := &outcomeEntryRepo{status: string(conversation.ConversationStatusNew)}
	svc := outcomeServiceWith(t, repo, nil)
	runs := &agentRunCancellerSpy{}
	svc.SetAgentRunCanceller(runs)

	if err := svc.SetConversationStatus("entry-1", string(shared.EntryTypeWhatsApp), conversation.ConversationStatusOngoing); err != nil {
		t.Fatal(err)
	}
	finished := &outcomeEntryRepo{status: string(conversation.ConversationStatusFinished)}
	again := outcomeServiceWith(t, finished, nil)
	again.SetAgentRunCanceller(runs)
	if err := again.Finish("entry-1", string(shared.EntryTypeWhatsApp), conversation.FinishOptions{}); err != nil {
		t.Fatal(err)
	}
	if len(runs.calls) != 0 {
		t.Fatalf("cancel calls = %v, want none for a status change or a repeated finish", runs.calls)
	}
}

func TestACancelFailureNeverBlocksTheFinish(t *testing.T) {
	repo := &outcomeEntryRepo{status: string(conversation.ConversationStatusOngoing)}
	svc := outcomeServiceWith(t, repo, nil)
	svc.SetAgentRunCanceller(&agentRunCancellerSpy{err: errors.New("db down")})

	if err := svc.Finish("entry-1", string(shared.EntryTypeWhatsApp), conversation.FinishOptions{}); err != nil {
		t.Fatalf("the finish must stand: %v", err)
	}
	if len(repo.writes) != 1 {
		t.Fatalf("writes = %+v", repo.writes)
	}
}
