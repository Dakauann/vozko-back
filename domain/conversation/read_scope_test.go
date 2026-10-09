package conversation

import (
	"errors"
	"reflect"
	"testing"
)

func TestDepartmentAccessScope_ReadScope(t *testing.T) {
	restricted := DepartmentAccessScope{DepartmentIDs: []string{"d1", "d2"}, Restrict: true, WorkspaceHasDepartments: true}
	open := DepartmentAccessScope{WorkspaceHasDepartments: true}
	flat := DepartmentAccessScope{}

	cases := []struct {
		name     string
		scope    DepartmentAccessScope
		isAdmin  bool
		selected string
		want     ReadScope
		wantErr  error
	}{
		{"restricted member reads own departments and own assignments", restricted, false, "", ReadScope{DepartmentIDs: []string{"d1", "d2"}, RestrictDepartments: true, AssigneeOverrideUserID: "u1"}, nil},
		{"restricted admin reads the departments without the override", restricted, true, "", ReadScope{DepartmentIDs: []string{"d1", "d2"}, RestrictDepartments: true}, nil},
		{"restricted member narrows to an own department", restricted, false, "d2", ReadScope{DepartmentIDs: []string{"d2"}, RestrictDepartments: true, AssigneeOverrideUserID: "u1"}, nil},
		{"restricted member cannot pick a foreign department", restricted, false, "d9", ReadScope{}, ErrDepartmentOutsideScope},
		{"unrestricted member reads everything", open, false, "", ReadScope{}, nil},
		{"unrestricted member narrows to a picked department", open, false, "d9", ReadScope{DepartmentIDs: []string{"d9"}, RestrictDepartments: true, AssigneeOverrideUserID: "u1"}, nil},
		{"a workspace without departments ignores the pick", flat, false, "d9", ReadScope{}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.scope.ReadScope("u1", tc.isAdmin, tc.selected)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("want %v, got %v", tc.wantErr, err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("want %+v, got %+v", tc.want, got)
			}
		})
	}
}
