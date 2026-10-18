package unofficial_whatsapp

import (
	"context"
	"errors"
	"testing"
	"time"

	uw "vozko/domain/unofficial_whatsapp"
)

const scopedGroupJID = "120363000000000001@g.us"

func scopedGroupUseCases() *GroupUseCases {
	support := "support"
	synced := time.Now().UTC()
	instance := &uw.Instance{ID: "inst-1", WorkspaceID: "ws-1", ServerID: "srv-1", DepartmentID: &support, Status: uw.StatusConnected}
	group := &uw.Group{ID: "g1", WorkspaceID: "ws-1", InstanceID: "inst-1", JID: scopedGroupJID, Subject: "Suporte", WeAreAdmin: true, SyncedAt: &synced}
	return NewGroupUseCases(GroupUseCaseDeps{
		Instances: newFakeInstanceRepo(instance),
		Servers:   newFakeServerRepo(healthyServer("srv-1", 10, 1)),
		Groups:    newFakeGroupRepo(group),
		GroupAPI:  &fakeGroupAPI{},
	})
}

func salesOnly() uw.DepartmentScope {
	return uw.DepartmentScope{Restrict: true, DepartmentIDs: []string{"sales"}}
}

func TestGroupsOfAnotherDepartmentsNumberAreNotFound(t *testing.T) {
	uc := scopedGroupUseCases()
	ctx := context.Background()

	if _, err := uc.List(ctx, "ws-1", "inst-1", salesOnly()); !errors.Is(err, uw.ErrInstanceNotFound) {
		t.Fatalf("List = %v, want not found", err)
	}
	if _, err := uc.Get(ctx, "ws-1", "inst-1", scopedGroupJID, false, salesOnly()); !errors.Is(err, uw.ErrInstanceNotFound) {
		t.Fatalf("Get = %v, want not found", err)
	}
	if _, err := uc.InviteLink(ctx, "ws-1", "inst-1", scopedGroupJID, salesOnly()); !errors.Is(err, uw.ErrInstanceNotFound) {
		t.Fatalf("InviteLink = %v, want not found", err)
	}
	if _, err := uc.UpdateName(ctx, UpdateNameInput{WorkspaceID: "ws-1", InstanceID: "inst-1", GroupJID: scopedGroupJID, Name: "Novo", Scope: salesOnly()}); !errors.Is(err, uw.ErrInstanceNotFound) {
		t.Fatalf("UpdateName = %v, want not found", err)
	}
	if err := uc.Leave(ctx, "ws-1", "inst-1", scopedGroupJID, salesOnly()); !errors.Is(err, uw.ErrInstanceNotFound) {
		t.Fatalf("Leave = %v, want not found", err)
	}
}

func TestGroupsOfTheCallersDepartmentStayReadable(t *testing.T) {
	uc := scopedGroupUseCases()
	support := uw.DepartmentScope{Restrict: true, DepartmentIDs: []string{"support"}}

	group, err := uc.Get(context.Background(), "ws-1", "inst-1", scopedGroupJID, false, support)
	if err != nil || group.Subject != "Suporte" {
		t.Fatalf("Get = %+v, %v", group, err)
	}
	if _, err := uc.List(context.Background(), "ws-1", "inst-1", uw.Unrestricted()); err != nil {
		t.Fatalf("unrestricted List = %v", err)
	}
}
