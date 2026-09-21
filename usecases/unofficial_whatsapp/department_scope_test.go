package unofficial_whatsapp

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/shared"
	uw "vozko/domain/unofficial_whatsapp"
)

func deptOf(id string) *string { return &id }

func scopedTo(ids ...string) uw.DepartmentScope {
	return uw.DepartmentScope{DepartmentIDs: ids, Restrict: true}
}

func TestGetInstanceHidesAnotherDepartmentsNumber(t *testing.T) {
	instances := newFakeInstanceRepo(&uw.Instance{
		ID: "inst-a", WorkspaceID: "ws-1", DepartmentID: deptOf("dept-a"),
	})
	uc := NewGetInstanceUseCase(instances)

	_, err := uc.Execute(context.Background(), "inst-a", "ws-1", scopedTo("dept-b"))
	if !errors.Is(err, uw.ErrInstanceNotFound) {
		t.Fatalf("err = %v, want ErrInstanceNotFound", err)
	}
}

func TestGetInstanceAllowsOwnDepartment(t *testing.T) {
	instances := newFakeInstanceRepo(&uw.Instance{
		ID: "inst-a", WorkspaceID: "ws-1", DepartmentID: deptOf("dept-a"),
	})
	uc := NewGetInstanceUseCase(instances)

	instance, err := uc.Execute(context.Background(), "inst-a", "ws-1", scopedTo("dept-a"))
	if err != nil {
		t.Fatalf("a member of dept-a was refused their own number: %v", err)
	}
	if instance.ID != "inst-a" {
		t.Errorf("got %q", instance.ID)
	}
}

func TestGetInstanceUnrestrictedSeesEverything(t *testing.T) {
	instances := newFakeInstanceRepo(
		&uw.Instance{ID: "inst-a", WorkspaceID: "ws-1", DepartmentID: deptOf("dept-a")},
		&uw.Instance{ID: "inst-none", WorkspaceID: "ws-1"},
	)
	uc := NewGetInstanceUseCase(instances)

	for _, id := range []string{"inst-a", "inst-none"} {
		if _, err := uc.Execute(context.Background(), id, "ws-1", uw.Unrestricted()); err != nil {
			t.Errorf("unrestricted caller refused %s: %v", id, err)
		}
	}
}

func TestUnscopedNumberIsHiddenFromRestrictedMembers(t *testing.T) {
	instances := newFakeInstanceRepo(&uw.Instance{ID: "inst-none", WorkspaceID: "ws-1"})
	uc := NewGetInstanceUseCase(instances)

	if _, err := uc.Execute(context.Background(), "inst-none", "ws-1", scopedTo("dept-a")); !errors.Is(err, uw.ErrInstanceNotFound) {
		t.Fatalf("err = %v, want the number hidden", err)
	}
}

func TestTenancyStillEnforcedAlongsideDepartments(t *testing.T) {
	instances := newFakeInstanceRepo(&uw.Instance{
		ID: "inst-a", WorkspaceID: "other-ws", DepartmentID: deptOf("dept-a"),
	})
	uc := NewGetInstanceUseCase(instances)

	if _, err := uc.Execute(context.Background(), "inst-a", "ws-1", scopedTo("dept-a")); !errors.Is(err, uw.ErrInstanceNotFound) {
		t.Fatalf("err = %v; a matching department must not cross a workspace boundary", err)
	}
}

func TestStartConversationRefusesAnotherDepartmentsNumber(t *testing.T) {
	messaging := &fakeMessaging{}
	uc := NewStartConversationUseCase(
		newFakeInstanceRepo(&uw.Instance{
			ID: "inst-a", WorkspaceID: "ws-1", Status: uw.StatusConnected,
			DepartmentID: deptOf("dept-a"),
		}),
		newFakeServerRepo(&uw.Server{ID: "srv-1", BaseURL: "https://host.test"}),
		newFakeContactRepo(), newFakeConversationRepo(), messaging, nil)

	_, err := uc.Execute(context.Background(), StartConversationInput{
		WorkspaceID: "ws-1", InstanceID: "inst-a",
		PhoneNumber: "5511999999999",
		Scope:       scopedTo("dept-b"),
	})
	if !errors.Is(err, uw.ErrInstanceNotFound) {
		t.Fatalf("err = %v, want the number hidden", err)
	}
	if len(messaging.chatDetailCalls()) != 0 || len(messaging.texts) != 0 {
		t.Error("a refused cold outbound still reached the provider")
	}
}

func TestStartConversationAllowsOwnDepartment(t *testing.T) {
	uc := NewStartConversationUseCase(
		newFakeInstanceRepo(&uw.Instance{
			ID: "inst-a", WorkspaceID: "ws-1", ServerID: "srv-1",
			Status: uw.StatusConnected, DepartmentID: deptOf("dept-a"),
		}),
		newFakeServerRepo(&uw.Server{ID: "srv-1", BaseURL: "https://host.test"}),
		newFakeContactRepo(), newFakeConversationRepo(), &fakeMessaging{}, nil)

	started, err := uc.Execute(context.Background(), StartConversationInput{
		WorkspaceID: "ws-1", InstanceID: "inst-a",
		PhoneNumber: "5511999999999",
		Scope:       scopedTo("dept-a"),
	})
	if err != nil {
		t.Fatalf("a member of dept-a was refused their own number: %v", err)
	}
	if started == nil || started.ConversationID == "" {
		t.Error("no conversation was opened")
	}
}

func TestUpdateRefusesAnotherDepartmentsNumber(t *testing.T) {
	instances := newFakeInstanceRepo(&uw.Instance{
		ID: "inst-a", WorkspaceID: "ws-1", DepartmentID: deptOf("dept-a"),
	})
	uc := NewUpdateInstanceConfigUseCase(instances)

	stolen := deptOf("dept-b")
	_, err := uc.Execute(context.Background(), UpdateInstanceConfigInput{
		InstanceID: "inst-a", WorkspaceID: "ws-1",
		DepartmentID: &stolen,
		Scope:        scopedTo("dept-b"),
	})
	if !errors.Is(err, uw.ErrInstanceNotFound) {
		t.Fatalf("err = %v; a member reassigned a number out of another department", err)
	}
}

func TestDeleteRefusesAnotherDepartmentsNumber(t *testing.T) {
	instances := newFakeInstanceRepo(&uw.Instance{
		ID: "inst-a", WorkspaceID: "ws-1", ServerID: "srv-1", DepartmentID: deptOf("dept-a"),
	})
	uc := NewDeleteInstanceUseCase(
		instances, newFakeServerRepo(healthyServer("srv-1", 10, 1)), &fakeProvider{})

	err := uc.Execute(context.Background(), "inst-a", "ws-1", scopedTo("dept-b"))
	if !errors.Is(err, uw.ErrInstanceNotFound) {
		t.Fatalf("err = %v, want the delete refused", err)
	}
	if len(instances.deleted) != 0 {
		t.Error("the number was deleted despite the refusal")
	}
}

func TestConnectRefusesAnotherDepartmentsNumber(t *testing.T) {
	provider := &fakeProvider{}
	uc := NewConnectInstanceUseCase(
		newFakeInstanceRepo(&uw.Instance{
			ID: "inst-a", WorkspaceID: "ws-1", ServerID: "srv-1", DepartmentID: deptOf("dept-a"),
		}),
		newFakeServerRepo(healthyServer("srv-1", 10, 1)), provider)

	_, err := uc.Connect(context.Background(), ConnectRequest{
		InstanceID: "inst-a", WorkspaceID: "ws-1", Scope: scopedTo("dept-b"),
	})
	if !errors.Is(err, uw.ErrInstanceNotFound) {
		t.Fatalf("err = %v, want the link refused", err)
	}
}

func TestListPassesTheScopeThrough(t *testing.T) {
	instances := &scopeRecordingRepo{fakeInstanceRepo: newFakeInstanceRepo()}
	uc := NewListInstancesUseCase(instances)

	scope := scopedTo("dept-a", "dept-c")
	if _, err := uc.Execute(context.Background(), uw.ListInstancesInput{
		WorkspaceID: "ws-1", Scope: scope,
	}); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if !instances.lastInput.Scope.Restrict {
		t.Error("the scope reached the repository unrestricted; every number would be listed")
	}
	if len(instances.lastInput.Scope.DepartmentIDs) != 2 {
		t.Errorf("departments = %v, want both", instances.lastInput.Scope.DepartmentIDs)
	}
}

func TestZeroScopeIsUnrestricted(t *testing.T) {
	var zero uw.DepartmentScope
	if zero.Restrict {
		t.Fatal("the zero DepartmentScope restricts; internal callers would see nothing")
	}
	if !zero.Allows(deptOf("dept-a")) {
		t.Error("the zero scope refused a scoped number")
	}
}

type scopeRecordingRepo struct {
	*fakeInstanceRepo
	lastInput uw.ListInstancesInput
}

func (r *scopeRecordingRepo) ListByWorkspace(
	_ context.Context,
	in uw.ListInstancesInput,
) (*shared.PaginatedResult[*uw.Instance], error) {
	r.lastInput = in
	return shared.NewPaginatedResult([]*uw.Instance{}, in.Options.Pagination, 0), nil
}
