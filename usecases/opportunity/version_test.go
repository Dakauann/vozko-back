package opportunity_usecase

import (
	"errors"
	"testing"

	"vozko/domain/opportunity"
)

func TestSavingFromAnOldCopyIsRefusedAndWritesNothing(t *testing.T) {
	repo := newFakeOppRepo()
	svc := newService(repo)
	created, err := svc.Create("ws1", baseCreate())
	if err != nil {
		t.Fatal(err)
	}
	seen := created.Version

	value := int64(1500)
	if _, err := svc.Update("ws1", created.ID, UpdateInput{ValueCents: &value, ExpectedVersion: &seen}, "u1"); err != nil {
		t.Fatalf("first save error = %v", err)
	}

	other := int64(9900)
	if _, err := svc.Update("ws1", created.ID, UpdateInput{ValueCents: &other, ExpectedVersion: &seen}, "u1"); !errors.Is(err, opportunity.ErrStaleDeal) {
		t.Fatalf("save from the old copy error = %v, want ErrStaleDeal", err)
	}
	if repo.store[created.ID].ValueCents != 1500 {
		t.Fatalf("the refused save changed the deal: %d", repo.store[created.ID].ValueCents)
	}
	if _, err := svc.MoveStage("ws1", created.ID, MoveStageInput{StageID: "stage1", ExpectedVersion: &seen}, "u1"); !errors.Is(err, opportunity.ErrStaleDeal) {
		t.Fatalf("move from the old copy error = %v, want ErrStaleDeal", err)
	}
}

func TestEverySaveMovesTheVersionForward(t *testing.T) {
	repo := newFakeOppRepo()
	svc := newService(repo)
	created, _ := svc.Create("ws1", baseCreate())
	value := int64(10)
	updated, err := svc.Update("ws1", created.ID, UpdateInput{ValueCents: &value}, "u1")
	if err != nil || updated.Version != created.Version+1 {
		t.Fatalf("version %d -> %v, %v", created.Version, updated, err)
	}
}

func TestAutomationLosesARaceInsteadOfOverwriting(t *testing.T) {
	repo := newFakeOppRepo()
	svc := newAutomationService(repo)
	first, _ := svc.ManageForEntry("ws1", entryCommand(EntryCreate))
	repo.bumpBehindTheScenes = first.Opportunity.ID

	cmd := entryCommand(EntryUpdateValue)
	bigger := int64(20000)
	cmd.ValueCents = &bigger
	if _, err := svc.ManageForEntry("ws1", cmd); !errors.Is(err, opportunity.ErrStaleDeal) {
		t.Fatalf("automation over a concurrent change error = %v, want ErrStaleDeal", err)
	}
	if refusal, ok := RefusalOf(opportunity.ErrStaleDeal); !ok || refusal != RefusalDealChanged {
		t.Fatalf("stale deals must be a worded refusal, got %q", refusal)
	}
}
