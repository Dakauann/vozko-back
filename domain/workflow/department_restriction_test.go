package workflow

import (
	"reflect"
	"testing"

	wd "vozko/domain/workspace/workspace_department"
)

func TestDepartmentRestrictionFailsClosedWhenTheScopeIsMissingOrEmpty(t *testing.T) {
	selected := "dep-2"
	cases := []struct {
		name       string
		filter     *wd.DepartmentFilter
		restricted bool
		ids        []string
		nothing    bool
	}{
		{"no filter read", nil, true, nil, true},
		{"owner sees the whole workspace", &wd.DepartmentFilter{IsOwnerOrAdmin: true, WorkspaceHasDepartments: true}, false, nil, false},
		{"owner narrowed to a selected department", &wd.DepartmentFilter{IsOwnerOrAdmin: true, SelectedDepartmentID: &selected}, true, []string{"dep-2"}, false},
		{"workspace without departments", &wd.DepartmentFilter{}, false, nil, false},
		{"member of two departments", &wd.DepartmentFilter{DepartmentIDs: []string{"dep-1", "dep-3"}, WorkspaceHasDepartments: true}, true, []string{"dep-1", "dep-3"}, false},
		{"member of no department", &wd.DepartmentFilter{WorkspaceHasDepartments: true}, true, nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := DepartmentRestrictionOf(tc.filter)
			if got.Restricted != tc.restricted || !reflect.DeepEqual(got.DepartmentIDs, tc.ids) || got.Nothing() != tc.nothing {
				t.Fatalf("restriction = %+v nothing = %v", got, got.Nothing())
			}
		})
	}
}
