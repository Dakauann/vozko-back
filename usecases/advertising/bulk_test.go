package advertising

import (
	"context"
	"errors"
	"testing"

	ads "vozko/domain/advertising"
)

type scriptedManager struct {
	objects      map[string]*ads.Object
	refuse       map[string]error
	edits        map[string]ads.ObjectEdit
	checkedEdits map[string]ads.ObjectEdit
	checked      []string
}

func (m *scriptedManager) CheckStatus(_ context.Context, _, id string, _ bool) (*ads.Object, error) {
	if err := m.refuse[id]; err != nil {
		return nil, err
	}
	o, ok := m.objects[id]
	if !ok {
		return nil, ads.ErrObjectNotFound
	}
	return o, nil
}

func (m *scriptedManager) SetStatus(_ context.Context, _, id string, on bool) (*ads.Object, error) {
	if err := m.refuse[id]; err != nil {
		return nil, err
	}
	o := *m.objects[id]
	o.Status = ads.StatusPaused
	if on {
		o.Status = ads.StatusActive
	}
	return &o, nil
}

func (m *scriptedManager) Detail(_ context.Context, _, id string) (*ads.ObjectDetail, error) {
	o, ok := m.objects[id]
	if !ok {
		return nil, ads.ErrObjectNotFound
	}
	return &ads.ObjectDetail{Object: o, Budget: o.Budget()}, nil
}

func (m *scriptedManager) CheckEdit(_ context.Context, _, id string, edit ads.ObjectEdit) (*ads.ObjectDetail, error) {
	m.checked = append(m.checked, id)
	m.checkedEdits[id] = edit
	return &ads.ObjectDetail{Object: m.objects[id]}, nil
}

func (m *scriptedManager) Edit(_ context.Context, _, id string, edit ads.ObjectEdit) (*ads.Object, error) {
	m.edits[id] = edit
	o := *m.objects[id]
	if edit.Name != nil {
		o.Name = *edit.Name
	}
	return &o, nil
}

func newScriptedManager() *scriptedManager {
	return &scriptedManager{
		objects: map[string]*ads.Object{
			"c-1": {MetaID: "c-1", Level: ads.LevelCampaign, Name: "Promo Natal"},
			"c-2": {MetaID: "c-2", Level: ads.LevelCampaign, Name: "Institucional"},
		},
		refuse:       map[string]error{},
		edits:        map[string]ads.ObjectEdit{},
		checkedEdits: map[string]ads.ObjectEdit{},
	}
}

func TestBulkStatusReportsEachObject(t *testing.T) {
	m := newScriptedManager()
	m.refuse["c-2"] = ads.ErrNoFundingSource
	results, err := NewBulkUseCase(m).SetStatus(context.Background(), "ws", []string{"c-1", "c-2"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if results[0].Err != nil || results[0].Object.Status != ads.StatusActive || !errors.Is(results[1].Err, ads.ErrNoFundingSource) {
		t.Fatalf("got %+v", results)
	}
}

func TestBulkEditSkipsObjectsTheChangeDoesNotTouch(t *testing.T) {
	m := newScriptedManager()
	change := ads.BulkChange{Field: ads.BulkName, Mode: ads.BulkReplace, Find: "Natal", Replace: "Verão"}
	results, err := NewBulkUseCase(m).Edit(context.Background(), "ws", []string{"c-1", "c-2"}, change)
	if err != nil {
		t.Fatal(err)
	}
	if results[0].Object.Name != "Promo Verão" || !errors.Is(results[1].Err, ads.ErrNothingToChange) || len(m.edits) != 1 {
		t.Fatalf("got %+v edits %+v", results, m.edits)
	}
}

func TestBulkRefusesAnInvalidRequestBeforeTouchingMeta(t *testing.T) {
	m := newScriptedManager()
	if _, err := NewBulkUseCase(m).Edit(context.Background(), "ws", []string{"c-1"}, ads.BulkChange{Field: ads.BulkName, Mode: ads.BulkSet}); err == nil {
		t.Fatal("an empty name must be refused")
	}
	if _, err := NewBulkUseCase(m).SetStatus(context.Background(), "ws", nil, true); err == nil {
		t.Fatal("no targets must be refused")
	}
	if len(m.edits) != 0 {
		t.Fatal("nothing may change")
	}
}

func TestBulkApplySendsTheSameEditToEveryObject(t *testing.T) {
	m := newScriptedManager()
	name := "Black Friday"
	results, err := NewBulkUseCase(m).Apply(context.Background(), "ws", []string{"c-1", "c-2"}, ads.ObjectEdit{Name: &name})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 || results[0].Object.Name != "Black Friday" || results[1].Object.Name != "Black Friday" || len(m.edits) != 2 {
		t.Fatalf("got %+v", results)
	}
	if _, err := NewBulkUseCase(m).Apply(context.Background(), "ws", []string{"c-1"}, ads.ObjectEdit{}); !errors.Is(err, ads.ErrNothingToChange) {
		t.Fatalf("an empty edit must be refused, got %v", err)
	}
}

func TestCheckingABulkEditChangesNothing(t *testing.T) {
	m := newScriptedManager()
	change := ads.BulkChange{Field: ads.BulkName, Mode: ads.BulkReplace, Find: "Natal", Replace: "Verão"}
	results, err := NewBulkUseCase(m).CheckEdit(context.Background(), "ws", []string{"c-1", "c-2"}, change)
	if err != nil {
		t.Fatal(err)
	}
	if results[0].Err != nil || !errors.Is(results[1].Err, ads.ErrNothingToChange) || len(m.edits) != 0 || len(m.checked) != 1 {
		t.Fatalf("results %+v checked %v edits %v", results, m.checked, m.edits)
	}
}

func TestCheckingBulkStatusTouchesNothingAndNamesEachRefusal(t *testing.T) {
	m := newScriptedManager()
	m.refuse["c-2"] = ads.ErrNoFundingSource
	results, err := NewBulkUseCase(m).CheckStatus(context.Background(), "ws", []string{"c-1", "c-2"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if results[0].Err != nil || results[0].Object.Name != "Promo Natal" || !errors.Is(results[1].Err, ads.ErrNoFundingSource) || len(m.edits) != 0 {
		t.Fatalf("got %+v", results)
	}
	if _, err := NewBulkUseCase(m).CheckStatus(context.Background(), "ws", []string{"c-1", "c-1"}, true); err == nil {
		t.Fatal("repeated targets must be refused")
	}
}

func TestABulkBudgetKeepsTheKindOfEachItem(t *testing.T) {
	m := newScriptedManager()
	m.objects["s-1"] = &ads.Object{MetaID: "s-1", Level: ads.LevelAdSet, DailyBudget: 3000}
	m.objects["s-2"] = &ads.Object{MetaID: "s-2", Level: ads.LevelAdSet, LifetimeBudget: 90000}
	edit := ads.ObjectEdit{Budget: &ads.Budget{Amount: 4000}}
	ids := []string{"s-1", "s-2"}
	if _, err := NewBulkUseCase(m).CheckApply(context.Background(), "ws", ids, edit); err != nil {
		t.Fatal(err)
	}
	if *m.checkedEdits["s-1"].Budget != (ads.Budget{Kind: ads.BudgetDaily, Amount: 4000}) || *m.checkedEdits["s-2"].Budget != (ads.Budget{Kind: ads.BudgetLifetime, Amount: 4000}) || len(m.edits) != 0 {
		t.Fatalf("checked %+v edits %+v", m.checkedEdits, m.edits)
	}
	if _, err := NewBulkUseCase(m).Apply(context.Background(), "ws", ids, edit); err != nil {
		t.Fatal(err)
	}
	if m.edits["s-1"].Budget.Kind != ads.BudgetDaily || m.edits["s-2"].Budget.Kind != ads.BudgetLifetime {
		t.Fatalf("edits %+v", m.edits)
	}
	if _, err := NewBulkUseCase(m).CheckApply(context.Background(), "ws", ids, ads.ObjectEdit{}); !errors.Is(err, ads.ErrNothingToChange) {
		t.Fatalf("an empty edit must be refused, got %v", err)
	}
}
