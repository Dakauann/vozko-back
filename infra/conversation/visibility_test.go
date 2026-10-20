package conversation

import (
	"testing"

	"vozko/domain/conversation"
	"vozko/domain/inbox_assignment"
	"vozko/domain/shared"
	"vozko/domain/workspace"
	workspace_department "vozko/domain/workspace/workspace_department"
)

type memberOfAnotherDepartment struct{}

func (memberOfAnotherDepartment) ListDepartments(workspaceID string) ([]workspace_department.Department, error) {
	return []workspace_department.Department{{ID: testDepartment, WorkspaceID: workspaceID}, {ID: "dept-2", WorkspaceID: workspaceID}}, nil
}

func (memberOfAnotherDepartment) GetMemberDepartmentIDs(string, string) ([]string, error) {
	return []string{"dept-2"}, nil
}

func TestTheConversationVisibilityPortIsTheInboxEntryRule(t *testing.T) {
	sameDepartment := func(assignment *inbox_assignment.InboxAssignment, viewOthers bool) *Authorizer {
		return newTestAuthorizer(assignment, viewOthers)
	}
	otherDepartment := func(assignment *inbox_assignment.InboxAssignment, viewOthers bool) *Authorizer {
		perms := map[workspace.Action]bool{workspace.ActionViewOthers: viewOthers}
		return NewAuthorizer(stubEntryAccess{}, &stubPlacements{}, stubMembership{permissions: perms}, memberOfAnotherDepartment{},
			&stubAssignments{assignment: assignment}, stubResolver{}, &stubShared{data: map[string]string{}}).(*Authorizer)
	}
	cases := []struct {
		name string
		auth *Authorizer
		want bool
	}{
		{"visible in the viewer's department", sameDepartment(nil, false), true},
		{"hidden by department", otherDepartment(nil, true), false},
		{"visible outside the department when assigned to the viewer", otherDepartment(assignedTo(viewer), false), true},
		{"hidden by ownership without view_others", sameDepartment(assignedTo(owner), false), false},
		{"visible with view_others", sameDepartment(assignedTo(owner), true), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := conversation.NewVisibility(tc.auth).CanView(shared.Person{UserID: viewer}, testWorkspace, testEntry, testEntryType)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("CanView = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestTheListScopeOfTheVisibilityPortMatchesTheInboxList(t *testing.T) {
	cases := []struct {
		name       string
		viewOthers bool
		want       conversation.VisibilityScope
	}{
		{"department member with view_others", true, conversation.VisibilityScope{ReadScope: conversation.ReadScope{DepartmentIDs: []string{testDepartment}, RestrictDepartments: true, AssigneeOverrideUserID: viewer}}},
		{"department member without view_others", false, conversation.VisibilityScope{ReadScope: conversation.ReadScope{DepartmentIDs: []string{testDepartment}, RestrictDepartments: true, AssigneeOverrideUserID: viewer}, AssignedUserID: viewer}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := conversation.NewVisibility(newTestAuthorizer(nil, tc.viewOthers)).Scope(shared.Person{UserID: viewer}, testWorkspace)
			if err != nil {
				t.Fatal(err)
			}
			if got.RestrictDepartments != tc.want.RestrictDepartments || got.AssigneeOverrideUserID != tc.want.AssigneeOverrideUserID ||
				got.AssignedUserID != tc.want.AssignedUserID || len(got.DepartmentIDs) != 1 || got.DepartmentIDs[0] != testDepartment {
				t.Fatalf("scope = %+v, want %+v", got, tc.want)
			}
		})
	}
}
