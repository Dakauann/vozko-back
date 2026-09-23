package conversation

import (
	"testing"
	"time"

	"vozko/domain/cache"
	"vozko/domain/inbox_assignment"
	"vozko/domain/workspace"
	workspace_department "vozko/domain/workspace/workspace_department"
)

const (
	testWorkspace  = "ws-1"
	testEntry      = "entry-1"
	testEntryType  = "whatsapp"
	testDepartment = "dept-1"
	viewer         = "user-viewer"
	owner          = "user-owner"
)

type stubShared struct {
	cache.SharedState
	data map[string]string
}

func (s *stubShared) Exists(key string) (bool, error) {
	_, ok := s.data[key]
	return ok, nil
}

func (s *stubShared) SetString(key, value string, _ time.Duration) error {
	s.data[key] = value
	return nil
}

type stubEntryAccess struct{}

func (stubEntryAccess) CanUserAccessEntry(string, string, bool) (bool, error) { return true, nil }
func (stubEntryAccess) GetAccessibleEntryIDs(string, bool) ([]string, error)  { return nil, nil }

type stubMembership struct {
	permissions map[workspace.Action]bool
}

func (m stubMembership) GetMember(workspaceID, userID string) (*workspace.Member, error) {
	return &workspace.Member{ID: "member-" + userID, WorkspaceID: workspaceID, UserID: userID}, nil
}

func (m stubMembership) HasPermission(_ string, resource workspace.Resource, action workspace.Action) (bool, error) {
	if resource != workspace.ResourceConversations {
		return false, nil
	}
	if action == workspace.ActionRead {
		return true, nil
	}
	return m.permissions[action], nil
}

type stubDepartments struct{}

func (stubDepartments) ListDepartments(workspaceID string) ([]workspace_department.Department, error) {
	return []workspace_department.Department{{ID: testDepartment, WorkspaceID: workspaceID}}, nil
}

func (stubDepartments) GetMemberDepartmentIDs(string, string) ([]string, error) {
	return []string{testDepartment}, nil
}

type stubAssignments struct {
	assignment *inbox_assignment.InboxAssignment
}

func (s *stubAssignments) FindByEntry(string, string, string) (*inbox_assignment.InboxAssignment, error) {
	return s.assignment, nil
}

type stubResolver struct{}

func (stubResolver) GetCampaignWorkspaceID(string, string) (string, error) { return testWorkspace, nil }
func (stubResolver) GetCampaignDepartmentID(string, string) (string, error) {
	return testDepartment, nil
}
func (stubResolver) GetEntryWorkspaceID(string, string) (string, error)  { return testWorkspace, nil }
func (stubResolver) GetEntryDepartmentID(string, string) (string, error) { return testDepartment, nil }
func (stubResolver) GetEntryCampaignID(string, string) (string, error)   { return "campaign-1", nil }

func newTestAuthorizer(assignment *inbox_assignment.InboxAssignment, canViewOthers bool) *Authorizer {
	perms := map[workspace.Action]bool{}
	if canViewOthers {
		perms[workspace.ActionViewOthers] = true
	}
	return NewAuthorizer(
		stubEntryAccess{},
		stubMembership{permissions: perms},
		stubDepartments{},
		&stubAssignments{assignment: assignment},
		stubResolver{},
		&stubShared{data: map[string]string{}},
	).(*Authorizer)
}

func assignedTo(userID string) *inbox_assignment.InboxAssignment {
	return &inbox_assignment.InboxAssignment{
		WorkspaceID:    testWorkspace,
		EntryID:        testEntry,
		EntryType:      testEntryType,
		AssignedUserID: userID,
	}
}

func TestCanAccessEntry_AssignmentScope(t *testing.T) {
	tests := []struct {
		name          string
		assignment    *inbox_assignment.InboxAssignment
		canViewOthers bool
		want          bool
	}{
		{"assigned to someone else without view_others", assignedTo(owner), false, false},
		{"assigned to someone else with view_others", assignedTo(owner), true, true},
		{"assigned to the caller", assignedTo(viewer), false, true},
		{"unassigned", nil, false, true},
		{"assignment row without an owner", assignedTo(""), false, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			a := newTestAuthorizer(tc.assignment, tc.canViewOthers)
			if got := a.CanAccessEntry(viewer, testWorkspace, testEntry, testEntryType, false); got != tc.want {
				t.Fatalf("CanAccessEntry = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestCanAccessEntry_CachedGrantDoesNotBypassAssignment(t *testing.T) {
	assignments := &stubAssignments{}
	a := NewAuthorizer(
		stubEntryAccess{},
		stubMembership{permissions: map[workspace.Action]bool{}},
		stubDepartments{},
		assignments,
		stubResolver{},
		&stubShared{data: map[string]string{}},
	).(*Authorizer)

	if !a.CanAccessEntry(viewer, testWorkspace, testEntry, testEntryType, false) {
		t.Fatal("expected access to an unassigned entry")
	}

	assignments.assignment = assignedTo(owner)

	if a.CanAccessEntry(viewer, testWorkspace, testEntry, testEntryType, false) {
		t.Fatal("expected the assignment gate to survive a cached grant")
	}
}

func TestCanAccessEntry_SystemAdminUnaffected(t *testing.T) {
	a := newTestAuthorizer(assignedTo(owner), false)
	if !a.CanAccessEntry(viewer, testWorkspace, testEntry, testEntryType, true) {
		t.Fatal("expected a system admin to keep access")
	}
}
