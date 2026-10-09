package lead_usecase

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"vozko/domain/actor"
	"vozko/domain/lead"
	"vozko/domain/shared"
)

func newIncomingFixture(t *testing.T, leads ...*lead.Lead) (*IncomingMerge, *fakeStore, *fakeNotifier) {
	t.Helper()
	store, notifier := newFakeStore(leads...), &fakeNotifier{}
	merge, err := NewIncomingMerge(store, notifier)
	if err != nil {
		t.Fatalf("NewIncomingMerge: %v", err)
	}
	return merge, store, notifier
}

func TestNewIncomingMergeRefusesAMissingDependency(t *testing.T) {
	if _, err := NewIncomingMerge(nil, &fakeNotifier{}); err == nil {
		t.Fatal("a merge without a store must not be built")
	}
	if _, err := NewIncomingMerge(newFakeStore(), nil); err == nil {
		t.Fatal("a merge without a notifier must not be built")
	}
}

func TestIncomingMergeWritesTheKnownLeadByIDWithItsEventAndNotifies(t *testing.T) {
	merge, store, notifier := newIncomingFixture(t, storedLead())

	got, err := merge.MergeIncoming(context.Background(), cmdWorkspace, "l-1", lead.LeadUpdate{Source: lead.SourceImport, Name: "Ana Souza"})
	if err != nil {
		t.Fatalf("MergeIncoming: %v", err)
	}
	if got.ID != "l-1" || got.Name != "Ana Souza" || got.NameSource != lead.SourceImport || got.Version != 4 {
		t.Fatalf("got %+v", got)
	}
	if len(store.saves) != 1 || store.saves[0].expected != 3 {
		t.Fatalf("saves = %+v", store.saves)
	}
	event := store.saves[0].events[0]
	if event.Kind != lead.EventMerged || event.Actor != actor.SystemID || !reflect.DeepEqual(event.Fields(), []string{lead.FieldName}) {
		t.Fatalf("event = %+v", event)
	}
	if len(notifier.changes) != 1 || notifier.changes[0].Version != 4 || !reflect.DeepEqual(notifier.changes[0].Fields, []string{lead.FieldName}) {
		t.Fatalf("notified = %+v", notifier.changes)
	}
}

func TestIncomingMergeNeverReplacesAManualName(t *testing.T) {
	manual := storedLead()
	manual.Name, manual.NameSource = "Ana Maria", lead.SourceManual
	merge, store, notifier := newIncomingFixture(t, manual)

	got, err := merge.MergeIncoming(context.Background(), cmdWorkspace, "l-1", lead.LeadUpdate{Source: lead.SourceImport, Name: "Ana Souza"})
	if err != nil || got.Name != "Ana Maria" || len(store.saves) != 0 || len(notifier.changes) != 0 {
		t.Fatalf("a manual name must stay and nothing is written: %+v, %v", got, err)
	}
}

func TestIncomingMergeRetriesAfterLosingARace(t *testing.T) {
	merge, store, _ := newIncomingFixture(t, storedLead())
	store.raceOnce = true
	got, err := merge.MergeIncoming(context.Background(), cmdWorkspace, "l-1", lead.LeadUpdate{Source: lead.SourceImport, Name: "Ana Souza"})
	if err != nil || got.Version != 5 || got.Name != "Ana Souza" {
		t.Fatalf("after a race = %+v, %v", got, err)
	}
}

func TestIncomingMergeRefuses(t *testing.T) {
	merge, _, _ := newIncomingFixture(t, storedLead())
	ctx := context.Background()
	cases := []struct {
		name        string
		workspaceID string
		leadID      string
		update      lead.LeadUpdate
		want        error
	}{
		{"a lead of another workspace", "ws-2", "l-1", lead.LeadUpdate{Source: lead.SourceImport, Name: "Ana"}, lead.ErrLeadNotFound},
		{"a lead that is gone", cmdWorkspace, "l-gone", lead.LeadUpdate{Source: lead.SourceImport, Name: "Ana"}, lead.ErrLeadNotFound},
		{"no workspace", "", "l-1", lead.LeadUpdate{Source: lead.SourceImport, Name: "Ana"}, lead.ErrLeadWorkspaceRequired},
		{"no lead", cmdWorkspace, "", lead.LeadUpdate{Source: lead.SourceImport, Name: "Ana"}, lead.ErrLeadRequired},
		{"no source", cmdWorkspace, "l-1", lead.LeadUpdate{Name: "Ana"}, lead.ErrLeadSourceInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := merge.MergeIncoming(ctx, tc.workspaceID, tc.leadID, tc.update); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestIncomingMergeThatKeepsLosingIsAConflict(t *testing.T) {
	merge, err := NewIncomingMerge(alwaysConflicting{newFakeStore(storedLead())}, &fakeNotifier{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := merge.MergeIncoming(context.Background(), cmdWorkspace, "l-1", lead.LeadUpdate{Source: lead.SourceImport, Name: "Ana Souza"}); !errors.Is(err, shared.ErrVersionConflict) {
		t.Fatalf("err = %v, want a version conflict", err)
	}
}
