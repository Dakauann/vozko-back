package workspace_department

import "testing"

func TestScopeDescribesWhatTheMemberCanSee(t *testing.T) {
	selected := "dept-1"

	cases := []struct {
		name   string
		filter *DepartmentFilter
		want   Scope
	}{
		{
			name:   "no departments in the workspace: permissions alone decide",
			filter: &DepartmentFilter{},
			want:   Scope{},
		},
		{
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
			name:   "owner or admin: never restricted, never blocked",
			filter: &DepartmentFilter{IsOwnerOrAdmin: true, WorkspaceHasDepartments: true},
			want:   Scope{WorkspaceUsesDepartments: true},
		},
		{
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

func TestScopeCarriesNoOneElsesInformation(t *testing.T) {
	scope := ScopeFor(&DepartmentFilter{
		WorkspaceHasDepartments: true,
		DepartmentIDs:           []string{"dept-secret-1", "dept-secret-2"},
	})
	if scope.MemberDepartmentCount != 2 {
		t.Fatalf("member department count = %d, want 2", scope.MemberDepartmentCount)
	}
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
