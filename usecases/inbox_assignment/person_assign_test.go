package inbox_assignment_usecase

import (
	"errors"
	"testing"

	"vozko/domain/conversation"
	ia "vozko/domain/inbox_assignment"
	"vozko/domain/shared"
)

type assignAccess bool

func (a assignAccess) CanAccessEntry(_, _, _, _ string, _ bool) bool { return bool(a) }

type targetScopes map[string]bool

func (t targetScopes) GetDepartmentScope(userID, _ string, _ bool) (conversation.DepartmentAccessScope, bool) {
	return conversation.DepartmentAccessScope{}, t[userID]
}

type reach bool

func (r reach) CanView(_, _, _ string, _ bool) (bool, error) { return bool(r), nil }

type phones struct{}

func (phones) BusinessPhoneForEntry(string, string) string { return "phone-1" }

type assignRecorder struct {
	calls    []string
	handOffs []ia.RouletteHandOff
}

func (a *assignRecorder) HandOffToRoulette(in ia.RouletteHandOff) (string, error) {
	a.handOffs = append(a.handOffs, in)
	return "u3", nil
}

func (a *assignRecorder) AssignManual(entryID, entryType, businessPhoneID, workspaceID, toUserID, assignedBy, trigger string) error {
	a.calls = append(a.calls, entryID+"|"+businessPhoneID+"|"+workspaceID+"|"+toUserID+"|"+assignedBy+"|"+trigger)
	return nil
}

func assignFixture(access bool, eligible bool, reachable bool, assigner *assignRecorder) ia.PersonAssignUseCase {
	return NewPersonAssignUseCase(PersonAssignDeps{
		Access:     assignAccess(access),
		Targets:    targetScopes{"u2": eligible},
		Visibility: reach(reachable),
		Phones:     phones{},
		Assigner:   assigner,
		Roulette:   assigner,
	})
}

func TestPersonAssignRefusesAConversationTheCallerCannotSee(t *testing.T) {
	assigner := &assignRecorder{}
	err := assignFixture(false, true, true, assigner).Assign(shared.Person{UserID: "u1"}, "ws1", "e1", "whatsapp", "u2")
	if !errors.Is(err, ia.ErrAssignEntryAccess) || len(assigner.calls) != 0 {
		t.Fatalf("err %v calls %v", err, assigner.calls)
	}
}

func TestPersonAssignRefusesAMemberWithoutConversationAccess(t *testing.T) {
	assigner := &assignRecorder{}
	err := assignFixture(true, false, true, assigner).Assign(shared.Person{UserID: "u1"}, "ws1", "e1", "whatsapp", "u2")
	if !errors.Is(err, ia.ErrAssignTargetIneligible) || len(assigner.calls) != 0 {
		t.Fatalf("err %v calls %v", err, assigner.calls)
	}
}

func TestPersonAssignRefusesAMemberOutsideTheCallersDepartments(t *testing.T) {
	assigner := &assignRecorder{}
	err := assignFixture(true, true, false, assigner).Assign(shared.Person{UserID: "u1"}, "ws1", "e1", "whatsapp", "u2")
	if !errors.Is(err, ia.ErrAssignTargetOutOfReach) || len(assigner.calls) != 0 {
		t.Fatalf("err %v calls %v", err, assigner.calls)
	}
}

func TestPersonAssignAssignsAsTheCaller(t *testing.T) {
	assigner := &assignRecorder{}
	if err := assignFixture(true, true, true, assigner).Assign(shared.Person{UserID: "u1"}, "ws1", "e1", "whatsapp", "u2"); err != nil {
		t.Fatal(err)
	}
	if assigner.calls[0] != "e1|phone-1|ws1|u2|u1|"+ia.TriggerManual {
		t.Fatalf("calls %v", assigner.calls)
	}
}

func TestHandOffRefusesAConversationTheCallerCannotSee(t *testing.T) {
	assigner := &assignRecorder{}
	if _, err := assignFixture(false, true, true, assigner).HandOff(shared.Person{UserID: "u1"}, "ws1", "e1", "whatsapp", "d1"); !errors.Is(err, ia.ErrAssignEntryAccess) || len(assigner.handOffs) != 0 {
		t.Fatalf("err %v handOffs %v", err, assigner.handOffs)
	}
}

func TestHandOffDealsFromTheDepartmentRingAsTheCaller(t *testing.T) {
	assigner := &assignRecorder{}
	owner, err := assignFixture(true, true, true, assigner).HandOff(shared.Person{UserID: "u1"}, "ws1", "e1", "whatsapp", "d1")
	if err != nil || owner != "u3" {
		t.Fatalf("owner %q err %v", owner, err)
	}
	if got := assigner.handOffs[0]; got.DepartmentID != "d1" || got.ByActorID != "u1" || got.WorkspaceID != "ws1" {
		t.Fatalf("hand-off %+v", got)
	}
}
