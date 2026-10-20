package whatsapp_campaign

import (
	"reflect"
	"testing"

	dept "vozko/domain/workspace/workspace_department"
)

func TestScopedReportAccessNarrowsToWhatTheScopeAllows(t *testing.T) {
	owner := &dept.DepartmentFilter{IsOwnerOrAdmin: true}
	member := &dept.DepartmentFilter{DepartmentIDs: []string{"d1", "d2"}, WorkspaceHasDepartments: true}
	stranded := &dept.DepartmentFilter{WorkspaceHasDepartments: true}

	cases := []struct {
		name        string
		scope       *dept.DepartmentFilter
		requested   string
		departments []string
		blocked     bool
	}{
		{"an owner without a choice reads every department", owner, "", nil, false},
		{"an owner choosing a department reads only it", owner, "d9", []string{"d9"}, false},
		{"a member without a choice reads their own departments", member, "", []string{"d1", "d2"}, false},
		{"a member choosing their department reads only it", member, "d2", []string{"d2"}, false},
		{"a member choosing another department reads nothing", member, "d9", nil, true},
		{"a member in no department reads nothing", stranded, "", nil, true},
		{"no scope reads nothing", nil, "", nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			access := ScopedReportAccess("ws1", "", tc.requested, tc.scope)
			if access.WorkspaceID != "ws1" || access.AllowDepartment == nil {
				t.Fatalf("access = %+v", access)
			}
			if !reflect.DeepEqual(access.DepartmentIDs, tc.departments) || access.DepartmentsBlocked != tc.blocked {
				t.Fatalf("departments = %v blocked = %v, want %v %v", access.DepartmentIDs, access.DepartmentsBlocked, tc.departments, tc.blocked)
			}
		})
	}
}

func TestScopedReportAccessJudgesACampaignByTheScope(t *testing.T) {
	member := &dept.DepartmentFilter{DepartmentIDs: []string{"d1"}, WorkspaceHasDepartments: true}

	access := ScopedReportAccess("ws1", "c1", "", member)
	if access.CampaignID != "c1" || !access.AllowDepartment("d1") || access.AllowDepartment("d2") {
		t.Fatalf("a member must reach only campaigns of their departments: %+v", access)
	}
	if ScopedReportAccess("ws1", "c1", "", nil).AllowDepartment("d1") {
		t.Fatal("no scope must reach no campaign")
	}
}
