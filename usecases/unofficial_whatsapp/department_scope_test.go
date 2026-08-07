package unofficial_whatsapp

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/shared"
	uw "vozko/domain/unofficial_whatsapp"
)

// Department scoping at the endpoints that act on ONE number.
//
// The conversation half — inbox visibility, round-robin, opening a chat — is the
// platform's channel-neutral machinery and is exercised where it lives. What is
// tested here is the half that had no channel-neutral home: the number itself.
// Every one of these endpoints could previously be reached by an operator
// outside the department that owns the number.

func deptOf(id string) *string { return &id }

func scopedTo(ids ...string) uw.DepartmentScope {
	return uw.DepartmentScope{DepartmentIDs: ids, Restrict: true}
}

// A number belonging to another department is NOT FOUND, not forbidden.
//
// Whether a number exists inside a department you are not in is itself
// information — which teams exist, how many numbers they run — and an operator
// has no legitimate use for it.
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

// An owner or admin is unrestricted and sees every number, scoped or not.
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

// A number with NO department is hidden from a restricted member.
//
// Fail-closed, and it matches the inbox exactly: `= ANY(...)` never matches a
// NULL, so they already cannot read its conversations. Listing a number they can
// neither open nor answer from would only raise questions.
func TestUnscopedNumberIsHiddenFromRestrictedMembers(t *testing.T) {
	instances := newFakeInstanceRepo(&uw.Instance{ID: "inst-none", WorkspaceID: "ws-1"})
	uc := NewGetInstanceUseCase(instances)

	if _, err := uc.Execute(context.Background(), "inst-none", "ws-1", scopedTo("dept-a")); !errors.Is(err, uw.ErrInstanceNotFound) {
		t.Fatalf("err = %v, want the number hidden", err)
	}
}

// Tenancy still wins: another workspace's number is invisible whatever the
// department scope says.
func TestTenancyStillEnforcedAlongsideDepartments(t *testing.T) {
	instances := newFakeInstanceRepo(&uw.Instance{
		ID: "inst-a", WorkspaceID: "other-ws", DepartmentID: deptOf("dept-a"),
	})
	uc := NewGetInstanceUseCase(instances)

	if _, err := uc.Execute(context.Background(), "inst-a", "ws-1", scopedTo("dept-a")); !errors.Is(err, uw.ErrInstanceNotFound) {
		t.Fatalf("err = %v; a matching department must not cross a workspace boundary", err)
	}
}

// ---------------------------------------------------------------- cold outbound

// Starting a conversation from another department's number is refused.
//
// The sharpest case: cold outbound messages someone who never wrote in, from a
// number that belongs to a specific team, and it is the fastest way to get that
// number banned. Someone outside the department must not be able to spend it.
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
	// The refusal must land BEFORE the provider is touched: verifying the
	// number costs a call on an instance this caller may not use at all.
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

// ---------------------------------------------------------------- mutations

// Editing another department's number is refused — including the department
// field itself, which is how a member could otherwise pull another team's
// number into their own reach or push their own out of it.
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

// Deleting another department's number is refused. Removing a number releases
// its host slot and disconnects a live WhatsApp session — not something one team
// may do to another's.
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

// Linking is refused too: it is what turns a provisioned slot into a live
// session, so it belongs to the department that owns the number.
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

// ---------------------------------------------------------------- listing

// A restricted member's list is filtered to their departments. The repository
// does the filtering, so this pins the INPUT the usecase hands it — the place a
// forgotten field would silently widen the result.
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

// An internal caller — the health cron, capacity reconciliation, the entitlement
// count — is unrestricted by construction, because the zero scope is
// "unrestricted". Pinned because the opposite default would silently blind
// those jobs to every department-scoped number.
func TestZeroScopeIsUnrestricted(t *testing.T) {
	var zero uw.DepartmentScope
	if zero.Restrict {
		t.Fatal("the zero DepartmentScope restricts; internal callers would see nothing")
	}
	if !zero.Allows(deptOf("dept-a")) {
		t.Error("the zero scope refused a scoped number")
	}
}

// scopeRecordingRepo captures the list input the usecase built.
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
