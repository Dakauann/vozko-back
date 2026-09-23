package attendance_target

import (
	"testing"

	"vozko/domain/attendance"
)

func targetsFixture() []Target {
	return []Target{
		{Scope: ScopeWorkspace, MetricKey: attendance.MetricFinished, Value: 1000},
		{Scope: ScopeDepartment, ScopeID: "dept1", MetricKey: attendance.MetricFinished, Value: 400},
		{Scope: ScopeMember, ScopeID: "u1", MetricKey: attendance.MetricFinished, Value: 120},
		{Scope: ScopeWorkspace, MetricKey: attendance.MetricNewLeads, Value: 5000},
	}
}

func TestResolvePrefersTheNarrowestScope(t *testing.T) {
	cases := []struct {
		name     string
		selector Selector
		want     float64
		scope    Scope
	}{
		{"member beats department and workspace", Selector{DepartmentID: "dept1", MemberID: "u1"}, 120, ScopeMember},
		{"department beats workspace", Selector{DepartmentID: "dept1"}, 400, ScopeDepartment},
		{"workspace is the fallback", Selector{}, 1000, ScopeWorkspace},
		{"an unknown department falls back to the workspace", Selector{DepartmentID: "dept9"}, 1000, ScopeWorkspace},
		{"an unknown member falls back to the department", Selector{DepartmentID: "dept1", MemberID: "u9"}, 400, ScopeDepartment},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, found := Resolve(targetsFixture(), attendance.MetricFinished, tc.selector)
			if !found {
				t.Fatalf("Resolve() found = false, want true")
			}
			if got.Value != tc.want {
				t.Fatalf("Resolve() Value = %v, want %v", got.Value, tc.want)
			}
			if got.Scope != tc.scope {
				t.Fatalf("Resolve() Scope = %q, want %q", got.Scope, tc.scope)
			}
		})
	}
}

func TestResolveUnsetMetricIsAbsentNotZero(t *testing.T) {
	got, found := Resolve(targetsFixture(), attendance.MetricAvgFRTMins, Selector{})
	if found {
		t.Fatalf("Resolve() on an unset metric found = true (value %v), want false", got.Value)
	}
}

func TestFilterVisibleHidesOtherDepartments(t *testing.T) {
	targets := targetsFixture()
	targets = append(targets, Target{Scope: ScopeDepartment, ScopeID: "dept2", MetricKey: attendance.MetricFinished, Value: 700})

	got := FilterVisible(targets, []string{"dept1"}, true)
	for _, target := range got {
		if target.Scope == ScopeDepartment && target.ScopeID != "dept1" {
			t.Fatalf("FilterVisible() leaked %q to a member scoped to dept1", target.ScopeID)
		}
	}
	if len(got) != len(targets)-1 {
		t.Fatalf("FilterVisible() returned %d targets, want %d", len(got), len(targets)-1)
	}
}

func TestFilterVisibleIsAPassthroughWhenUnrestricted(t *testing.T) {
	targets := targetsFixture()
	got := FilterVisible(targets, nil, false)
	if len(got) != len(targets) {
		t.Fatalf("FilterVisible() unrestricted returned %d targets, want %d", len(got), len(targets))
	}
}

func TestFilterVisibleWithNoDepartmentsHidesEveryDepartmentTarget(t *testing.T) {
	got := FilterVisible(targetsFixture(), nil, true)
	for _, target := range got {
		if target.Scope == ScopeDepartment {
			t.Fatalf("FilterVisible() kept %q for a member with no departments", target.ScopeID)
		}
	}
}
