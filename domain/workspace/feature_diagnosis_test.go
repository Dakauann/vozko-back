package workspace

import (
	"strings"
	"testing"
)

func kanbanReader() Grants {
	return NewGrants(RoleMember, false, []PermissionEntry{
		{Resource: ResourceConversations, Action: ActionRead},
		{Resource: ResourceStages, Action: ActionRead},
	})
}

func statusOf(t *testing.T, d FeatureDiagnosis, key CapabilityKey) CapabilityStatus {
	t.Helper()
	for _, s := range d.Capabilities {
		if s.Key == key {
			return s
		}
	}
	t.Fatalf("capability %q missing from diagnosis", key)
	return CapabilityStatus{}
}

func findingKinds(d FeatureDiagnosis) map[ScopeFindingKind]ScopeFinding {
	out := map[ScopeFindingKind]ScopeFinding{}
	for _, f := range d.Scope {
		out[f.Kind] = f
	}
	return out
}

func TestDiagnosisSeparatesSeeingFromActing(t *testing.T) {
	board, _ := FeatureByKey("crm_board")
	d := board.Diagnose(kanbanReader(), MemberScope{})
	if !statusOf(t, d, "crm_board.view").Allowed {
		t.Error("the board must be visible with read permissions")
	}
	move := statusOf(t, d, "crm_board.move_stage")
	if move.Allowed || len(move.Missing) != 1 || move.Missing[0].Key() != "stages:assign" {
		t.Errorf("moving must be blocked by stages:assign only, got %+v", move)
	}
	if d.FullAccess || d.Name != "Kanban de conversas" || d.Location == "" {
		t.Errorf("diagnosis header is wrong: %+v", d)
	}
}

func TestDiagnosisExplainsAMemberOutsideEveryDepartment(t *testing.T) {
	board, _ := FeatureByKey("crm_board")
	d := board.Diagnose(kanbanReader(), MemberScope{WorkspaceUsesDepartments: true})
	kinds := findingKinds(d)
	if _, ok := kinds[ScopeNoDepartment]; !ok {
		t.Errorf("a member without a department must be warned, got %+v", d.Scope)
	}
	if _, ok := kinds[ScopeAssignedOnly]; !ok {
		t.Errorf("without view_others the board shows only assigned conversations, got %+v", d.Scope)
	}
}

func TestDiagnosisNamesTheDepartmentsWhenKnown(t *testing.T) {
	board, _ := FeatureByKey("crm_board")
	scope := MemberScope{WorkspaceUsesDepartments: true, DepartmentCount: 2, DepartmentNames: []string{"Comercial", "Suporte"}}
	own, ok := findingKinds(board.Diagnose(kanbanReader(), scope))[ScopeOwnDepartments]
	if !ok || !strings.Contains(own.Description, "Comercial, Suporte") {
		t.Errorf("own departments finding must list the names, got %+v", own)
	}
	hidden, _ := findingKinds(board.Diagnose(kanbanReader(), MemberScope{WorkspaceUsesDepartments: true, DepartmentCount: 2}))[ScopeOwnDepartments]
	if strings.Contains(hidden.Description, "Comercial") || hidden.Description == "" {
		t.Errorf("without names the finding stays generic, got %+v", hidden)
	}
}

func TestViewOthersLiftsTheAssignedOnlyScope(t *testing.T) {
	board, _ := FeatureByKey("crm_board")
	g := NewGrants(RoleMember, false, []PermissionEntry{
		{Resource: ResourceConversations, Action: ActionRead},
		{Resource: ResourceConversations, Action: ActionViewOthers},
		{Resource: ResourceStages, Action: ActionRead},
	})
	if _, ok := findingKinds(board.Diagnose(g, MemberScope{}))[ScopeAssignedOnly]; ok {
		t.Error("view_others must lift the assigned-only restriction")
	}
}

func TestManagersHaveFullAccessAndNoScope(t *testing.T) {
	board, _ := FeatureByKey("crm_board")
	d := board.Diagnose(NewGrants(RoleAdmin, false, nil), MemberScope{WorkspaceUsesDepartments: true})
	if !d.FullAccess || len(d.Scope) != 0 {
		t.Errorf("admins see everything, got %+v", d)
	}
	for _, s := range d.Capabilities {
		if !s.Allowed {
			t.Errorf("admin blocked on %q", s.Key)
		}
	}
}

func TestFeaturesWithoutScopesReportNoScope(t *testing.T) {
	funnels, _ := FeatureByKey("funnels")
	d := funnels.Diagnose(NewGrants(RoleMember, false, nil), MemberScope{WorkspaceUsesDepartments: true})
	if len(d.Scope) != 0 {
		t.Errorf("funnels are not department scoped, got %+v", d.Scope)
	}
}

func TestManagerOnlyCapabilitiesAreFlaggedForMembers(t *testing.T) {
	phones, _ := FeatureByKey("business_phones")
	connect := statusOf(t, phones.Diagnose(NewGrants(RoleMember, false, nil), MemberScope{}), "business_phones.connect")
	if connect.Allowed || !connect.ManagersOnly || len(connect.Missing) != 0 {
		t.Errorf("connecting numbers is reserved to owners and admins, got %+v", connect)
	}
}
