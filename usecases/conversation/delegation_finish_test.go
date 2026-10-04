package conversation_usecase

import (
	"context"
	"testing"

	"vozko/domain/conversation"
	"vozko/domain/shared"
)

type recordingDelegationStore struct {
	stubDelegations
	deleted []string
}

func (r *recordingDelegationStore) Delete(_ context.Context, entryID string, entryType shared.EntryType) error {
	r.deleted = append(r.deleted, string(entryType)+":"+entryID)
	return nil
}

func TestFinishingAConversationEndsItsDelegation(t *testing.T) {
	repo := &outcomeEntryRepo{status: string(conversation.ConversationStatusOngoing)}
	svc := outcomeServiceWith(t, repo, nil)
	store := &recordingDelegationStore{}
	svc.SetDelegations(store)

	if err := svc.Finish("entry-1", string(shared.EntryTypeWhatsApp), conversation.FinishOptions{
		Source: conversation.CloseSourceHuman, Reason: conversation.CloseReasonManual,
	}); err != nil {
		t.Fatal(err)
	}
	if len(store.deleted) != 1 || store.deleted[0] != "whatsapp:entry-1" {
		t.Fatalf("deleted = %v, want the finished conversation's delegation", store.deleted)
	}
}

func TestARefusedFinishKeepsTheDelegation(t *testing.T) {
	repo := &outcomeEntryRepo{status: string(conversation.ConversationStatusOngoing)}
	svc := outcomeServiceWith(t, repo, &stubCaptureReader{capture: requiringCapture()})
	store := &recordingDelegationStore{}
	svc.SetDelegations(store)

	if err := svc.Finish("entry-1", string(shared.EntryTypeWhatsApp), conversation.FinishOptions{
		Source: conversation.CloseSourceHuman, Reason: conversation.CloseReasonManual,
	}); err == nil {
		t.Fatal("expected the finish to be refused without an outcome")
	}
	if len(store.deleted) != 0 {
		t.Fatalf("a refused finish ended the delegation: %v", store.deleted)
	}
}
