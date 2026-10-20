package conversation

import (
	"testing"

	"vozko/domain/workspace"
)

type roleMembership struct {
	stubMembership
	role workspace.Role
}

func (m roleMembership) GetMember(workspaceID, userID string) (*workspace.Member, error) {
	member, err := m.stubMembership.GetMember(workspaceID, userID)
	if member != nil {
		member.Role = m.role
	}
	return member, err
}

func TestAnEntryAssignedToSomeoneElseIsReachedOnlyByWhoMaySeeOthers(t *testing.T) {
	cases := []struct {
		name       string
		role       workspace.Role
		viewOthers bool
		want       bool
	}{
		{"a member without view_others", workspace.RoleMember, false, false},
		{"a member with view_others", workspace.RoleMember, true, true},
		{"the workspace owner", workspace.RoleOwner, false, true},
		{"a workspace admin", workspace.RoleAdmin, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			perms := map[workspace.Action]bool{workspace.ActionViewOthers: tc.viewOthers}
			a := NewAuthorizer(
				stubEntryAccess{},
				&stubPlacements{},
				roleMembership{stubMembership: stubMembership{permissions: perms}, role: tc.role},
				stubDepartments{},
				&stubAssignments{assignment: assignedTo(owner)},
				stubResolver{},
				&stubShared{data: map[string]string{}},
			).(*Authorizer)

			if got := a.CanAccessEntry(viewer, testWorkspace, testEntry, testEntryType, false); got != tc.want {
				t.Fatalf("CanAccessEntry = %v, want %v", got, tc.want)
			}
		})
	}
}
