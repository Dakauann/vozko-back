package conversation

import (
	"testing"

	"vozko/domain/inbox_assignment"
	"vozko/domain/workspace"
	workspace_department "vozko/domain/workspace/workspace_department"
)

type countingMembership struct {
	stubMembership
	members int
}

func (m *countingMembership) GetMember(workspaceID, userID string) (*workspace.Member, error) {
	m.members++
	return m.stubMembership.GetMember(workspaceID, userID)
}

type countingDepartments struct {
	stubDepartments
	lists int
}

func (d *countingDepartments) ListDepartments(workspaceID string) ([]workspace_department.Department, error) {
	d.lists++
	return d.stubDepartments.ListDepartments(workspaceID)
}

func countingAuthorizer(assignment *inbox_assignment.InboxAssignment) (*Authorizer, *countingMembership, *countingDepartments) {
	members := &countingMembership{stubMembership: stubMembership{permissions: map[workspace.Action]bool{workspace.ActionViewOthers: true}}}
	departments := &countingDepartments{}
	a := NewAuthorizer(
		stubEntryAccess{},
		&stubPlacements{},
		members,
		departments,
		&stubAssignments{assignment: assignment},
		stubResolver{},
		&stubShared{data: map[string]string{}},
	).(*Authorizer)
	return a, members, departments
}

func TestEntryAccessFor_ReadsTheActorScopeOnceForManyEntries(t *testing.T) {
	a, members, departments := countingAuthorizer(assignedTo(owner))

	access := a.EntryAccessFor(viewer, testWorkspace, false)
	for _, id := range []string{"e1", "e2", "e3", "e4"} {
		if !access.CanAccess(id, testEntryType) {
			t.Fatalf("entry %s must be reachable", id)
		}
	}

	if members.members > 2 || departments.lists != 1 {
		t.Fatalf("the actor scope must be read once, got %d member reads and %d department lists", members.members, departments.lists)
	}
}

func TestEntryAccessFor_AnswersLikeCanAccessEntry(t *testing.T) {
	cases := []struct {
		name       string
		assignment *inbox_assignment.InboxAssignment
		viewOthers bool
		userID     string
		workspace  string
		entryID    string
		isAdmin    bool
	}{
		{"assigned elsewhere without view_others", assignedTo(owner), false, viewer, testWorkspace, testEntry, false},
		{"assigned elsewhere with view_others", assignedTo(owner), true, viewer, testWorkspace, testEntry, false},
		{"assigned to the caller", assignedTo(viewer), false, viewer, testWorkspace, testEntry, false},
		{"no user", nil, true, "", testWorkspace, testEntry, false},
		{"no workspace", nil, true, viewer, "", testEntry, false},
		{"no entry", nil, true, viewer, testWorkspace, "", false},
		{"system admin", assignedTo(owner), false, viewer, testWorkspace, testEntry, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			want := newTestAuthorizer(tc.assignment, tc.viewOthers).CanAccessEntry(tc.userID, tc.workspace, tc.entryID, testEntryType, tc.isAdmin)
			got := newTestAuthorizer(tc.assignment, tc.viewOthers).EntryAccessFor(tc.userID, tc.workspace, tc.isAdmin).CanAccess(tc.entryID, testEntryType)
			if got != want {
				t.Fatalf("EntryAccessFor = %v, CanAccessEntry = %v", got, want)
			}
		})
	}
}
