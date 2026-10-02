package advertising

import (
	"errors"
	"testing"
	"time"
)

func adSetDetail() ObjectDetail {
	o := &Object{MetaID: "s-1", Level: LevelAdSet, Status: StatusActive, DailyBudget: 2000, DestinationType: "WHATSAPP", OptimizationGoal: "CONVERSATIONS"}
	return ObjectDetail{Object: o, Budget: o.Budget()}
}

func TestEmptyEditIsRefused(t *testing.T) {
	if err := (ObjectEdit{}).Validate(adSetDetail(), false, draftNow); !errors.Is(err, ErrNothingToChange) {
		t.Fatalf("err %v", err)
	}
}

func TestRenameAndBudgetOnAnAdSet(t *testing.T) {
	name := "Novo nome"
	e := ObjectEdit{Name: &name, Budget: &Budget{Kind: BudgetDaily, Amount: 5000}}
	if err := e.Validate(adSetDetail(), false, draftNow); err != nil {
		t.Fatalf("edit refused: %v", err)
	}
}

func TestBudgetKindCannotSwitchAfterCreation(t *testing.T) {
	e := ObjectEdit{Budget: &Budget{Kind: BudgetLifetime, Amount: 50_000}}
	requireIssues(t, e.Validate(adSetDetail(), false, draftNow), FieldIssue{"budget", "kind_locked"})
}

func TestEachFieldOnlyAtItsLevel(t *testing.T) {
	ad := ObjectDetail{Object: &Object{Level: LevelAd, Status: StatusActive, DestinationType: "WHATSAPP"}}
	e := ObjectEdit{Targeting: &Targeting{Locations: []GeoLocation{{Kind: LocationCountry, Key: "BR"}}, AgeMin: 18, AgeMax: 65}, Budget: &Budget{Kind: BudgetDaily, Amount: 1}}
	requireIssues(t, e.Validate(ad, false, draftNow), FieldIssue{"targeting", "not_for_level"}, FieldIssue{"budget", "not_for_level"})
	creative := imageCreative()
	if err := (ObjectEdit{Creative: &creative}).Validate(ad, false, draftNow); err != nil {
		t.Fatalf("creative swap refused: %v", err)
	}
}

func TestEditingAnArchivedObjectIsRefused(t *testing.T) {
	name := "x"
	d := adSetDetail()
	d.Object.Status = StatusArchived
	if err := (ObjectEdit{Name: &name}).Validate(d, false, draftNow); !errors.Is(err, ErrObjectLocked) {
		t.Fatalf("err %v", err)
	}
}

func TestBudgetEditRespectsTheHourlyLimit(t *testing.T) {
	d := adSetDetail()
	for i := 0; i < 4; i++ {
		d.Object.BudgetChanges = append(d.Object.BudgetChanges, draftNow.Add(-time.Duration(i)*time.Minute))
	}
	requireIssues(t, (ObjectEdit{Budget: &Budget{Kind: BudgetDaily, Amount: 3000}}).Validate(d, false, draftNow), FieldIssue{"budget", "too_many_changes"})
}

func TestCopyAndLifecycleRules(t *testing.T) {
	campaign := &Object{Level: LevelCampaign, Status: StatusActive}
	requireIssues(t, (CopyRequest{ParentID: "x"}).Validate(campaign), FieldIssue{"parentId", "not_for_level"})
	if err := (CopyRequest{DeepCopy: true, NameSuffix: " cópia"}).Validate(campaign); err != nil {
		t.Fatalf("copy refused: %v", err)
	}
	if err := LifecycleArchive.Check(&Object{Status: StatusArchived}); !errors.Is(err, ErrObjectLocked) {
		t.Fatal("archived twice")
	}
	if err := LifecycleDelete.Check(&Object{Status: StatusArchived}); err != nil {
		t.Fatalf("archived object cannot be deleted: %v", err)
	}
}
