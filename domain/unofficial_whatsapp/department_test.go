package unofficial_whatsapp

import "testing"

func deptPtr(id string) *string { return &id }

func TestDepartmentScopeAllows(t *testing.T) {
	cases := []struct {
		name       string
		scope      DepartmentScope
		department *string
		want       bool
		why        string
	}{
		{
			name:       "unrestricted sees a scoped number",
			scope:      Unrestricted(),
			department: deptPtr("dept-a"),
			want:       true,
		},
		{
			name:       "unrestricted sees an unscoped number",
			scope:      Unrestricted(),
			department: nil,
			want:       true,
		},

		{
			name:       "member sees their own department's number",
			scope:      DepartmentScope{DepartmentIDs: []string{"dept-a"}, Restrict: true},
			department: deptPtr("dept-a"),
			want:       true,
		},
		{
			name:       "member in several departments sees each of them",
			scope:      DepartmentScope{DepartmentIDs: []string{"dept-a", "dept-b"}, Restrict: true},
			department: deptPtr("dept-b"),
			want:       true,
		},
		{
			name:       "member does NOT see another department's number",
			scope:      DepartmentScope{DepartmentIDs: []string{"dept-a"}, Restrict: true},
			department: deptPtr("dept-b"),
			want:       false,
			why:        "this is the whole feature",
		},

		{
			name:       "member does not see an UNSCOPED number",
			scope:      DepartmentScope{DepartmentIDs: []string{"dept-a"}, Restrict: true},
			department: nil,
			want:       false,
			why: "matches the inbox SQL, where `= ANY(...)` never matches a NULL: " +
				"they cannot read its conversations, so listing the number only raises questions",
		},
		{
			name:       "an empty-string department is treated as none",
			scope:      DepartmentScope{DepartmentIDs: []string{"dept-a"}, Restrict: true},
			department: deptPtr(""),
			want:       false,
		},
		{
			name:       "member in NO department sees nothing",
			scope:      DepartmentScope{Restrict: true},
			department: deptPtr("dept-a"),
			want:       false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.scope.Allows(tc.department); got != tc.want {
				t.Errorf("Allows() = %v, want %v (%s)", got, tc.want, tc.why)
			}
		})
	}
}

func TestUnrestrictedSeesEverything(t *testing.T) {
	if !Unrestricted().Allows(deptPtr("anything")) {
		t.Error("Unrestricted() refused a scoped number")
	}
	if Unrestricted().BlocksEverything() {
		t.Error("Unrestricted() reports that it blocks everything")
	}
}

func TestBlocksEverything(t *testing.T) {
	cases := map[string]struct {
		scope DepartmentScope
		want  bool
	}{
		"restricted with no departments": {DepartmentScope{Restrict: true}, true},
		"restricted with departments":    {DepartmentScope{DepartmentIDs: []string{"d"}, Restrict: true}, false},
		"unrestricted":                   {Unrestricted(), false},
		"unrestricted with departments":  {DepartmentScope{DepartmentIDs: []string{"d"}}, false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := tc.scope.BlocksEverything(); got != tc.want {
				t.Errorf("BlocksEverything() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestAllowsInstance(t *testing.T) {
	restricted := DepartmentScope{DepartmentIDs: []string{"dept-a"}, Restrict: true}

	if !restricted.AllowsInstance(&Instance{DepartmentID: deptPtr("dept-a")}) {
		t.Error("refused an instance in the caller's own department")
	}
	if restricted.AllowsInstance(&Instance{DepartmentID: deptPtr("dept-b")}) {
		t.Error("allowed an instance from another department")
	}
	if restricted.AllowsInstance(nil) {
		t.Error("a nil instance was allowed; absence must never read as permission")
	}
	if Unrestricted().AllowsInstance(nil) {
		t.Error("a nil instance was allowed even unrestricted")
	}
}
