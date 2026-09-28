package opportunity_usecase

import (
	"errors"
	"testing"

	"vozko/domain/workspace"
)

type assignAccessStub struct{ granted map[string]bool }

func (a assignAccessStub) Execute(userID, _ string, resource workspace.Resource, action workspace.Action) error {
	if resource == workspace.ResourceConversations && action == workspace.ActionAssign && a.granted[userID] {
		return nil
	}
	return workspace.ErrInsufficientPermissions
}

func TestPeopleNeedAssignToChooseAnotherOwner(t *testing.T) {
	svc := newService(newFakeOppRepo())
	in := baseCreate()
	in.Actor, in.OwnerID = "u3", "u2"
	if _, err := svc.Create("ws1", in); !errors.Is(err, ErrOwnerChoiceDenied) {
		t.Fatalf("create for someone else without assign error = %v", err)
	}

	in.OwnerID = "u3"
	created, err := svc.Create("ws1", in)
	if err != nil || created.OwnerID != "u3" {
		t.Fatalf("choosing yourself = %+v, %v", created, err)
	}

	other := "u2"
	if _, err := svc.Update("ws1", created.ID, UpdateInput{OwnerID: &other}, "u3"); !errors.Is(err, ErrOwnerChoiceDenied) {
		t.Fatalf("reassign without assign error = %v", err)
	}
	if _, err := svc.Update("ws1", created.ID, UpdateInput{OwnerID: &other, ActorIsPlatformAdmin: true}, "u3"); err != nil {
		t.Fatalf("platform admin reassign error = %v", err)
	}
}

func TestPeopleWithAssignAndAutomationChooseOwnersFreely(t *testing.T) {
	svc := newService(newFakeOppRepo())
	in := baseCreate()
	in.OwnerID = "u2"
	if _, err := svc.Create("ws1", in); err != nil {
		t.Fatalf("u1 holds assign, error = %v", err)
	}
	in.Actor = "ai:agent-1"
	if _, err := svc.Create("ws1", in); err != nil {
		t.Fatalf("automation actors are server controlled, error = %v", err)
	}
}
