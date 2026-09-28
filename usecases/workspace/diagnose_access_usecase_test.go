package workspace_usecase

import (
	"errors"
	"strings"
	"testing"

	"vozko/domain/workspace"
	workspace_department "vozko/domain/workspace/workspace_department"
)

type diagnoseRepo struct {
	*fakeWsRepo
	grants map[string][]workspace.PermissionEntry
}

func (r *diagnoseRepo) GetPermissions(memberID string) ([]*workspace.Permission, error) {
	out := make([]*workspace.Permission, 0)
	for _, p := range r.grants[memberID] {
		out = append(out, &workspace.Permission{MemberID: memberID, Resource: p.Resource, Action: p.Action})
	}
	return out, nil
}

type diagnoseDepartments struct {
	*fakeDeptRepo
	memberErr error
	byIDsHits int
}

func (d *diagnoseDepartments) GetMemberDepartmentIDs(workspaceID, userID string) ([]string, error) {
	if d.memberErr != nil {
		return nil, d.memberErr
	}
	return d.fakeDeptRepo.GetMemberDepartmentIDs(workspaceID, userID)
}

func (d *diagnoseDepartments) ListDepartmentsByIDs(ids []string) ([]workspace_department.Department, error) {
	d.byIDsHits++
	out := make([]workspace_department.Department, 0)
	for _, dept := range d.departments {
		for _, id := range ids {
			if dept.ID == id {
				out = append(out, dept)
			}
		}
	}
	return out, nil
}

var kanbanReadOnly = []workspace.PermissionEntry{
	{Resource: workspace.ResourceConversations, Action: workspace.ActionRead},
	{Resource: workspace.ResourceStages, Action: workspace.ActionRead},
}

type diagnoseFixture struct {
	repo  *diagnoseRepo
	depts *diagnoseDepartments
	uc    workspace.DiagnoseAccessUseCase
}

func newDiagnoseFixture() *diagnoseFixture {
	repo := &diagnoseRepo{
		fakeWsRepo: &fakeWsRepo{
			membersByUser: map[string]*workspace.Member{
				"owner": {ID: "m-owner", UserID: "owner", Role: workspace.RoleOwner},
				"ana":   {ID: "m-ana", UserID: "ana", Role: workspace.RoleMember, Username: "Ana"},
				"bia":   {ID: "m-bia", UserID: "bia", Role: workspace.RoleMember},
				"lead":  {ID: "m-lead", UserID: "lead", Role: workspace.RoleMember},
				"admin": {ID: "m-admin", UserID: "admin", Role: workspace.RoleAdmin},
			},
			perms: map[string]bool{
				permKey("m-lead", workspace.ResourceMembers, workspace.ActionRead): true,
			},
		},
		grants: map[string][]workspace.PermissionEntry{"m-ana": kanbanReadOnly, "m-bia": kanbanReadOnly},
	}
	depts := &diagnoseDepartments{fakeDeptRepo: &fakeDeptRepo{
		departments: []workspace_department.Department{{ID: "d1", Name: "Comercial"}, {ID: "d2", Name: "Suporte"}},
		deptsByUser: map[string][]string{"bia": {"d1"}},
	}}
	return &diagnoseFixture{
		repo:  repo,
		depts: depts,
		uc:    NewDiagnoseAccessUseCase(repo, NewGetMemberPermissionsUseCase(repo), depts),
	}
}

func (f *diagnoseFixture) run(actor, member string, feature workspace.FeatureKey) (*workspace.AccessDiagnosis, error) {
	return f.uc.Execute(workspace.DiagnoseAccessInput{ActorID: actor, WorkspaceID: ws, MemberUserID: member, CallerRole: "user", Feature: feature})
}

func capability(t *testing.T, d *workspace.AccessDiagnosis, key workspace.CapabilityKey) workspace.CapabilityStatus {
	t.Helper()
	for _, c := range d.Capabilities {
		if c.Key == key {
			return c
		}
	}
	t.Fatalf("capability %q missing", key)
	return workspace.CapabilityStatus{}
}

func scopeKinds(d *workspace.AccessDiagnosis) map[workspace.ScopeFindingKind]string {
	out := map[workspace.ScopeFindingKind]string{}
	for _, s := range d.Scope {
		out[s.Kind] = s.Description
	}
	return out
}

func TestDiagnoseExplainsTheEmptyKanbanOfAMemberOutsideEveryDepartment(t *testing.T) {
	d, err := newDiagnoseFixture().run("ana", "", "crm_board")
	if err != nil {
		t.Fatal(err)
	}
	if d.Member.UserID != "ana" {
		t.Fatalf("an empty member means the actor, got %q", d.Member.UserID)
	}
	if !capability(t, d, "crm_board.view").Allowed || capability(t, d, "crm_board.move_stage").Allowed {
		t.Error("ana sees the board but cannot move cards")
	}
	kinds := scopeKinds(d)
	if _, ok := kinds[workspace.ScopeNoDepartment]; !ok {
		t.Errorf("the missing department must be reported, got %v", kinds)
	}
}

func TestDiagnoseNamesTheActorsOwnDepartments(t *testing.T) {
	d, err := newDiagnoseFixture().run("bia", "bia", "crm_board")
	if err != nil {
		t.Fatal(err)
	}
	if desc := scopeKinds(d)[workspace.ScopeOwnDepartments]; desc == "" || !strings.Contains(desc, "Comercial") {
		t.Errorf("bia's own department must be named, got %q", desc)
	}
}

func TestDiagnosingSomeoneElseNeedsMembersRead(t *testing.T) {
	if _, err := newDiagnoseFixture().run("ana", "bia", "crm_board"); !errors.Is(err, workspace.ErrInsufficientPermissions) {
		t.Fatalf("a member without members:read cannot inspect others, got %v", err)
	}
}

func TestOtherMembersDepartmentNamesNeedDepartmentsRead(t *testing.T) {
	f := newDiagnoseFixture()
	d, err := f.run("lead", "bia", "crm_board")
	if err != nil {
		t.Fatal(err)
	}
	desc := scopeKinds(d)[workspace.ScopeOwnDepartments]
	if desc == "" || strings.Contains(desc, "Comercial") || f.depts.byIDsHits != 0 {
		t.Errorf("without departments:read the names stay hidden, got %q", desc)
	}

	f.repo.perms[permKey("m-lead", workspace.ResourceDepartments, workspace.ActionRead)] = true
	d, _ = f.run("lead", "bia", "crm_board")
	if !strings.Contains(scopeKinds(d)[workspace.ScopeOwnDepartments], "Comercial") {
		t.Error("with departments:read the names are shown")
	}
}

func TestOwnersSeeNamesAndAdminsHaveFullAccessWithoutScope(t *testing.T) {
	f := newDiagnoseFixture()
	d, err := f.run("owner", "bia", "crm_board")
	if err != nil || !strings.Contains(scopeKinds(d)[workspace.ScopeOwnDepartments], "Comercial") {
		t.Fatalf("the owner sees department names, got %v %v", d, err)
	}
	d, err = f.run("owner", "admin", "crm_board")
	if err != nil || !d.FullAccess || len(d.Scope) != 0 {
		t.Fatalf("an admin target has full access and no scope, got %+v %v", d, err)
	}
}

func TestDiagnoseRejectsUnknownFeaturesAndStrangers(t *testing.T) {
	f := newDiagnoseFixture()
	if _, err := f.run("ana", "", "not_a_feature"); !errors.Is(err, workspace.ErrUnknownFeature) {
		t.Errorf("unknown feature must fail, got %v", err)
	}
	if _, err := f.run("stranger", "", "crm_board"); !errors.Is(err, workspace.ErrUnauthorized) {
		t.Errorf("a non-member must be refused, got %v", err)
	}
	if _, err := f.run("owner", "ghost", "crm_board"); !errors.Is(err, workspace.ErrMemberNotFound) {
		t.Errorf("an unknown target must fail, got %v", err)
	}
}

func TestDiagnoseFailsClosedWhenDepartmentsCannotBeRead(t *testing.T) {
	f := newDiagnoseFixture()
	f.depts.memberErr = errors.New("db down")
	if _, err := f.run("ana", "", "crm_board"); err == nil {
		t.Fatal("a department lookup failure must not produce a diagnosis")
	}
}
