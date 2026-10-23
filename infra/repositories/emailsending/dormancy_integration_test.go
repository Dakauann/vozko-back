package emailsending_repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	es "vozko/domain/emailsending"
)

func TestIntegrationLastSentAtIsTheLatestSendOfTheWorkspace(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	messages := NewMessageRepository(db)
	activity := NewSendActivity(db)
	ws, other := uuid.NewString(), uuid.NewString()
	if got, err := activity.LastSentAt(ctx, ws); err != nil || got != nil {
		t.Fatalf("a workspace that never sent = %v, %v", got, err)
	}
	older := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	latest := time.Date(2026, 7, 9, 18, 30, 0, 0, time.UTC)
	seedMessage(t, messages, ws, uuid.NewString(), "ses-"+uuid.NewString(), older)
	seedMessage(t, messages, ws, uuid.NewString(), "ses-"+uuid.NewString(), latest)
	seedMessage(t, messages, other, uuid.NewString(), "ses-"+uuid.NewString(), latest.AddDate(0, 1, 0))
	got, err := activity.LastSentAt(ctx, ws)
	if err != nil || got == nil || !got.Equal(latest) {
		t.Fatalf("LastSentAt = %v, %v, want %v", got, err, latest)
	}
}

func TestIntegrationListByStatusFindsOnlyThatStatus(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	accounts := NewAccountRepository(db)
	for i, status := range []es.AccountStatus{es.AccountActive, es.AccountDormant, es.AccountPausedByProvider} {
		ws := uuid.NewString()
		acct := &es.SenderAccount{WorkspaceID: ws, Provider: "ses", TenantName: "ws-" + ws, Mode: es.ModeProbation, Status: status, Slug: "loja" + string(rune('a'+i))}
		if err := accounts.Create(ctx, acct); err != nil {
			t.Fatal(err)
		}
	}
	got, err := accounts.ListByStatus(ctx, es.AccountDormant)
	if err != nil || len(got) != 1 || got[0].Status != es.AccountDormant {
		t.Fatalf("ListByStatus = %+v, %v", got, err)
	}
}
