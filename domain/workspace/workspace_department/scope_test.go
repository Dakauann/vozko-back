package workspace_department

import "testing"

// The rule a support thread keeps rediscovering, pinned.
//
// Departments are a SCOPE, not a permission, and the two interact in a way that
// is invisible from either screen: a member with a completely unrestricted role
// sees nothing the moment the workspace has a department they are not in. These
// cases are the exact states the UI has to be able to tell apart.
func TestScopeDescribesWhatTheMemberCanSee(t *testing.T) {
	selected := "dept-1"

	cases := []struct {
		name   string
		filter *DepartmentFilter
		want   Scope
	}{
		{
			// The assumption everyone starts from, and it IS right until the
			// first department exists.
			name:   "no departments in the workspace: permissions alone decide",
			filter: &DepartmentFilter{},
			want:   Scope{},
		},
		{
			// The state that caused the confusion: a full role, and nothing on
			// screen, because the member is in no department.
			name:   "departments exist and the member is in none: sees nothing",
			filter: &DepartmentFilter{WorkspaceHasDepartments: true},
			want: Scope{
				WorkspaceUsesDepartments:   true,
				RestrictedToOwnDepartments: true,
				BlockedByMissingDepartment: true,
			},
		},
		{
			name:   "member of one department: scoped, but not blocked",
			filter: &DepartmentFilter{WorkspaceHasDepartments: true, DepartmentIDs: []string{"dept-1"}},
			want: Scope{
				WorkspaceUsesDepartments:   true,
				MemberDepartmentCount:      1,
				RestrictedToOwnDepartments: true,
			},
		},
		{
			// An admin is never scoped, so nothing must be explained to them
			// about their own visibility, even in a workspace full of
			// departments they do not belong to.
			name:   "owner or admin: never restricted, never blocked",
			filter: &DepartmentFilter{IsOwnerOrAdmin: true, WorkspaceHasDepartments: true},
			want:   Scope{WorkspaceUsesDepartments: true},
		},
		{
			// A selected department implies the workspace uses them, even if
			// the flag did not travel: the member picked one they belong to.
			name:   "a selected department implies the rule is in force",
			filter: &DepartmentFilter{DepartmentIDs: []string{"dept-1"}, SelectedDepartmentID: &selected},
			want: Scope{
				WorkspaceUsesDepartments:   true,
				MemberDepartmentCount:      1,
				RestrictedToOwnDepartments: true,
			},
		},
		{
			name:   "no filter at all",
			filter: nil,
			want:   Scope{},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ScopeFor(tc.filter); got != tc.want {
				t.Errorf("ScopeFor() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// The scope a member is handed must describe THEM and nobody else. A count is
// safe; the names of the workspace's departments are not this member's
// business, and a struct that grew one would leak it to the least privileged
// account in the workspace.
func TestScopeCarriesNoOneElsesInformation(t *testing.T) {
	scope := ScopeFor(&DepartmentFilter{
		WorkspaceHasDepartments: true,
		DepartmentIDs:           []string{"dept-secret-1", "dept-secret-2"},
	})
	if scope.MemberDepartmentCount != 2 {
		t.Fatalf("member department count = %d, want 2", scope.MemberDepartmentCount)
	}
	// Structural: Scope is a flat struct of bools and one int on purpose, so a
	// field carrying names or ids cannot be added without this failing.
	for _, field := range []any{
		scope.WorkspaceUsesDepartments,
		scope.MemberDepartmentCount,
		scope.RestrictedToOwnDepartments,
		scope.BlockedByMissingDepartment,
	} {
		switch field.(type) {
		case bool, int:
		default:
			t.Errorf("Scope carries a %T, which can hold more than a fact about the caller", field)
		}
	}
}
