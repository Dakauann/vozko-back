package label_usecase

import (
	"errors"
	"testing"

	"vozko/domain/label"
	"vozko/domain/shared"
)

type labelAccess bool

func (a labelAccess) CanAccessEntry(_, _, _, _ string, _ bool) bool { return bool(a) }

type applyStub struct{ inputs []label.AssignEntryLabelInput }

func (a *applyStub) Execute(_ string, in label.AssignEntryLabelInput) (*label.EntryLabel, error) {
	a.inputs = append(a.inputs, in)
	return &label.EntryLabel{LabelID: in.LabelID}, nil
}

type removeStub struct{ inputs []label.RemoveEntryLabelInput }

func (r *removeStub) Execute(_ string, in label.RemoveEntryLabelInput) error {
	r.inputs = append(r.inputs, in)
	return nil
}

func TestEntryLabelsRefuseAConversationThePersonCannotSee(t *testing.T) {
	apply, remove := &applyStub{}, &removeStub{}
	uc := NewEntryLabelsUseCase(labelAccess(false), apply, remove)
	someone := shared.Person{UserID: "u1"}
	if _, err := uc.Apply("ws1", someone, label.AssignEntryLabelInput{LabelID: "l1", EntryID: "e1", EntryType: "whatsapp"}); !errors.Is(err, label.ErrEntryAccess) {
		t.Fatalf("apply: %v", err)
	}
	if err := uc.Remove("ws1", someone, label.RemoveEntryLabelInput{LabelID: "l1", EntryID: "e1", EntryType: "whatsapp"}); !errors.Is(err, label.ErrEntryAccess) {
		t.Fatalf("remove: %v", err)
	}
	if len(apply.inputs)+len(remove.inputs) != 0 {
		t.Fatal("labelled without access")
	}
}

func TestEntryLabelsRecordTheRealPerson(t *testing.T) {
	apply, remove := &applyStub{}, &removeStub{}
	uc := NewEntryLabelsUseCase(labelAccess(true), apply, remove)
	someone := shared.Person{UserID: "u1"}
	if _, err := uc.Apply("ws1", someone, label.AssignEntryLabelInput{LabelID: "l1", EntryID: "e1", EntryType: "whatsapp", ActorID: "forged"}); err != nil {
		t.Fatal(err)
	}
	if err := uc.Remove("ws1", someone, label.RemoveEntryLabelInput{LabelID: "l1", EntryID: "e1", EntryType: "whatsapp", ActorID: "forged"}); err != nil {
		t.Fatal(err)
	}
	if apply.inputs[0].ActorID != "u1" || remove.inputs[0].ActorID != "u1" {
		t.Fatalf("actors %q %q", apply.inputs[0].ActorID, remove.inputs[0].ActorID)
	}
}
