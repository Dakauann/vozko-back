package lead_usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"vozko/domain/lead"
	"vozko/domain/shared"
)

func consentFixture(t *testing.T, leads ...*lead.Lead) (*ConsentCommands, *fakeStore, *fakeNotifier) {
	t.Helper()
	store, notifier := newFakeStore(leads...), &fakeNotifier{}
	ids := 0
	consent, err := NewConsentCommands(ConsentDeps{Store: store, Notifier: notifier, Now: func() time.Time { return cmdNow },
		NewID: func() string { ids++; return []string{"", "ev-1", "ev-2", "ev-3"}[ids] }})
	if err != nil {
		t.Fatal(err)
	}
	return consent, store, notifier
}

func keywordWithdrawal() lead.ConsentChange {
	return lead.ConsentChange{Kind: lead.ConsentWithdraw, Source: lead.ConsentKeyword,
		Actor: lead.ConsentActor{Type: lead.ConsentActorLead, ID: "l-1"}, Evidence: lead.ConsentEvidence{Type: lead.EvidenceMessage, Ref: "wamid.in", Excerpt: "SAIR"}}
}

func TestConsentCommandsNeedEveryDependency(t *testing.T) {
	for name, deps := range map[string]ConsentDeps{
		"no store":    {Notifier: &fakeNotifier{}},
		"no notifier": {Store: newFakeStore()},
	} {
		if _, err := NewConsentCommands(deps); err == nil {
			t.Fatalf("%s: want a refusal", name)
		}
	}
}

func TestRecordingAWithdrawalSavesItsEventAndAnnouncesIt(t *testing.T) {
	consent, store, notifier := consentFixture(t, storedLead())

	out, err := consent.Record(context.Background(), cmdWorkspace, "l-1", keywordWithdrawal())

	if err != nil || !out.Changed || out.EventID != "ev-1" {
		t.Fatalf("Record = %+v, %v", out, err)
	}
	if out.Lead.OptedOutAt == nil || !out.Lead.OptedOutAt.Equal(cmdNow) || out.Lead.OptOutSource != lead.OptOutLeadRequest {
		t.Fatalf("lead = %+v", out.Lead)
	}
	if len(store.saves) != 1 || len(store.saves[0].events) != 1 {
		t.Fatalf("saves = %+v", store.saves)
	}
	event := store.saves[0].events[0]
	if event.ID != "ev-1" || event.Kind != lead.EventConsentWithdrawn || event.Actor != "system" {
		t.Fatalf("event = %+v", event)
	}
	if len(notifier.changes) != 1 || notifier.changes[0].LeadID != "l-1" {
		t.Fatalf("notifications = %+v", notifier.changes)
	}
	if stored := store.leads["l-1"]; stored.ConsentRecord == nil || stored.ConsentRecord.EventID != "ev-1" {
		t.Fatalf("stored record = %+v", stored.ConsentRecord)
	}
}

func TestWithdrawingTwiceWritesAndEmitsNothing(t *testing.T) {
	consent, store, notifier := consentFixture(t, storedLead())
	if _, err := consent.Record(context.Background(), cmdWorkspace, "l-1", keywordWithdrawal()); err != nil {
		t.Fatal(err)
	}

	out, err := consent.Record(context.Background(), cmdWorkspace, "l-1", keywordWithdrawal())

	if err != nil || out.Changed || out.EventID != "" {
		t.Fatalf("second Record = %+v, %v", out, err)
	}
	if len(store.saves) != 1 || len(notifier.changes) != 1 {
		t.Fatalf("a repeated withdrawal wrote %d times and announced %d", len(store.saves), len(notifier.changes))
	}
}

func TestARefusedChangeIsNeverLoadedOrWritten(t *testing.T) {
	consent, store, _ := consentFixture(t, storedLead())
	grant := keywordWithdrawal()
	grant.Kind = lead.ConsentGrant

	if _, err := consent.Record(context.Background(), cmdWorkspace, "l-1", grant); !errors.Is(err, lead.ErrConsentNotAllowed) {
		t.Fatalf("a keyword grant = %v, want ErrConsentNotAllowed", err)
	}
	if _, err := consent.Record(context.Background(), cmdWorkspace, "missing", keywordWithdrawal()); !errors.Is(err, lead.ErrLeadNotFound) {
		t.Fatalf("an unknown lead = %v, want ErrLeadNotFound", err)
	}
	if _, err := consent.Record(context.Background(), "", "l-1", keywordWithdrawal()); !errors.Is(err, lead.ErrLeadWorkspaceRequired) {
		t.Fatalf("no workspace = %v", err)
	}
	if len(store.saves) != 0 {
		t.Fatal("nothing may be written")
	}
}

func TestAWithdrawalRetriesAfterLosingARace(t *testing.T) {
	consent, store, _ := consentFixture(t, storedLead())
	store.raceOnce = true

	out, err := consent.Record(context.Background(), cmdWorkspace, "l-1", keywordWithdrawal())

	if err != nil || !out.Changed || out.EventID != "ev-1" || len(store.saves) != 1 {
		t.Fatalf("Record after a race = %+v, %v (saves %d)", out, err, len(store.saves))
	}
}

func TestAWithdrawalThatKeepsLosingRacesFailsLoudly(t *testing.T) {
	store := newFakeStore(storedLead())
	consent, err := NewConsentCommands(ConsentDeps{Store: alwaysConflicting{store}, Notifier: &fakeNotifier{}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := consent.Record(context.Background(), cmdWorkspace, "l-1", keywordWithdrawal()); !errors.Is(err, shared.ErrVersionConflict) {
		t.Fatalf("err = %v, want a version conflict", err)
	}
}

func TestTheOptOutConfirmationIsClaimedOnceForTheWithdrawal(t *testing.T) {
	consent, store, _ := consentFixture(t, storedLead())
	out, err := consent.Record(context.Background(), cmdWorkspace, "l-1", keywordWithdrawal())
	if err != nil {
		t.Fatal(err)
	}

	if err := consent.ClaimOptOutConfirmation(context.Background(), cmdWorkspace, "l-1", out.EventID); err != nil {
		t.Fatalf("first claim = %v", err)
	}
	if err := consent.ClaimOptOutConfirmation(context.Background(), cmdWorkspace, "l-1", out.EventID); !errors.Is(err, lead.ErrLeadOptedOut) {
		t.Fatalf("second claim = %v, want ErrLeadOptedOut", err)
	}
	if lead.ErrorCode(lead.ErrLeadOptedOut) != "lead_opted_out" {
		t.Fatal("the refusal must carry lead_opted_out")
	}
	if stored := store.leads["l-1"]; stored.ConsentRecord.ConfirmationSentAt == nil {
		t.Fatal("the claim must be stored")
	}
	if len(store.saves) != 2 || len(store.saves[1].events) != 0 {
		t.Fatalf("the claim writes once and records no timeline event: %+v", store.saves)
	}
}

func TestAConfirmationForAnotherWithdrawalIsRefused(t *testing.T) {
	consent, store, _ := consentFixture(t, storedLead())
	if _, err := consent.Record(context.Background(), cmdWorkspace, "l-1", keywordWithdrawal()); err != nil {
		t.Fatal(err)
	}
	if err := consent.ClaimOptOutConfirmation(context.Background(), cmdWorkspace, "l-1", "ev-other"); !errors.Is(err, lead.ErrLeadOptedOut) {
		t.Fatalf("claim = %v", err)
	}
	if len(store.saves) != 1 {
		t.Fatal("a refused claim writes nothing")
	}
}
