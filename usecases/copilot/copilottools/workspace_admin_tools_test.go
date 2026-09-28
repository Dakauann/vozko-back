package copilottools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"vozko/domain/copilot"
	"vozko/domain/workspace"
	wd "vozko/domain/workspace/workspace_department"
)

const (
	userOwner   = "0a0a0a0a-0000-4000-8000-000000000001"
	userAdmin   = "0a0a0a0a-0000-4000-8000-000000000002"
	userAna     = "0a0a0a0a-0000-4000-8000-000000000003"
	userBia     = "0a0a0a0a-0000-4000-8000-000000000004"
	knownRole   = "0b0b0b0b-0000-4000-8000-000000000001"
	knownInvite = "0c0c0c0c-0000-4000-8000-000000000001"
	knownDept   = "0d0d0d0d-0000-4000-8000-000000000001"
	foreignDept = "0d0d0d0d-0000-4000-8000-000000000002"
)

var teamMembers = []*workspace.Member{
	{ID: "m-owner", UserID: userOwner, Username: "Dona", Email: "dona@loja.com", Role: workspace.RoleOwner},
	{ID: "m-admin", UserID: userAdmin, Username: "Admin", Email: "admin@loja.com", Role: workspace.RoleAdmin},
	{ID: "m-ana", UserID: userAna, Username: "Ana", Email: "ana@loja.com", Role: workspace.RoleMember},
	{ID: "m-bia", UserID: userBia, Username: "Bia", Email: "bia@loja.com", Role: workspace.RoleMember},
}

type fakeTeam struct{ workspace string }

func (f *fakeTeam) Execute(_, workspaceID, _ string) ([]*workspace.Member, error) {
	f.workspace = workspaceID
	return teamMembers, nil
}

type fakeInvites struct{}

func (fakeInvites) Execute(_, _, _ string) ([]*workspace.Invite, error) {
	return []*workspace.Invite{{ID: knownInvite, Email: "novo@loja.com", Role: workspace.RoleMember, Status: workspace.InviteStatusPending, Token: "secret-token"}}, nil
}

type fakeInvite struct{ in workspace.InviteMemberInput }

func (f *fakeInvite) Execute(_, _, _ string, in workspace.InviteMemberInput) (*workspace.Invite, error) {
	f.in = in
	return &workspace.Invite{ID: "i-new", Email: in.Email}, nil
}

type fakeMemberPermissions map[string][]*workspace.Permission

func (f fakeMemberPermissions) Execute(_, _, target, _ string) ([]*workspace.Permission, error) {
	return f[target], nil
}

type fakeSetPermissions struct {
	target string
	in     workspace.SetPermissionsInput
}

func (f *fakeSetPermissions) Execute(_, _, target, _ string, in workspace.SetPermissionsInput) ([]*workspace.Permission, error) {
	f.target, f.in = target, in
	out := make([]*workspace.Permission, 0, len(in.Permissions))
	for _, p := range in.Permissions {
		out = append(out, &workspace.Permission{Resource: p.Resource, Action: p.Action})
	}
	return out, nil
}

type fakeRoles struct{}

func (fakeRoles) Execute(_, _, _ string) ([]*workspace.CustomRole, error) {
	return []*workspace.CustomRole{{ID: knownRole, Name: "Atendente", Permissions: []workspace.PermissionEntry{
		{Resource: workspace.ResourceConversations, Action: workspace.ActionRead},
		{Resource: workspace.ResourceLabels, Action: workspace.ActionRead},
	}}}, nil
}

type fakeUpdateRole struct {
	roleID string
	in     workspace.UpdateCustomRoleInput
}

func (f *fakeUpdateRole) Execute(_, _, _, roleID string, in workspace.UpdateCustomRoleInput) (*workspace.CustomRole, error) {
	f.roleID, f.in = roleID, in
	return &workspace.CustomRole{ID: roleID, Name: "Atendente", Permissions: in.Permissions}, nil
}

type fakeScopedDepartments struct {
	added, removed []string
}

func (f *fakeScopedDepartments) owned(workspaceID, id string) error {
	if workspaceID != "ws-1" || id != knownDept {
		return wd.ErrDepartmentNotFound
	}
	return nil
}

func (f *fakeScopedDepartments) Get(workspaceID, id string) (*wd.Department, error) {
	if err := f.owned(workspaceID, id); err != nil {
		return nil, err
	}
	return &wd.Department{ID: id, WorkspaceID: workspaceID, Name: "Vendas"}, nil
}
func (f *fakeScopedDepartments) Update(workspaceID, id string, _ wd.UpdateDepartmentInput) (*wd.Department, error) {
	return f.Get(workspaceID, id)
}
func (f *fakeScopedDepartments) Delete(workspaceID, id string) error { return f.owned(workspaceID, id) }
func (f *fakeScopedDepartments) ListMembers(workspaceID, id string) ([]wd.DepartmentMember, error) {
	return nil, f.owned(workspaceID, id)
}
func (f *fakeScopedDepartments) AddMember(workspaceID, id string, in wd.AddMemberInput) (*wd.DepartmentMember, error) {
	if err := f.owned(workspaceID, id); err != nil {
		return nil, err
	}
	f.added = append(f.added, in.MemberID)
	return &wd.DepartmentMember{MemberID: in.MemberID}, nil
}
func (f *fakeScopedDepartments) RemoveMember(workspaceID, id, memberID string) error {
	f.removed = append(f.removed, memberID)
	return f.owned(workspaceID, id)
}

type adminFixture struct {
	deps        WorkspaceAdminDeps
	invite      *fakeInvite
	setPerms    *fakeSetPermissions
	updateRole  *fakeUpdateRole
	departments *fakeScopedDepartments
}

func newAdminFixture() adminFixture {
	f := adminFixture{invite: &fakeInvite{}, setPerms: &fakeSetPermissions{}, updateRole: &fakeUpdateRole{}, departments: &fakeScopedDepartments{}}
	f.deps = WorkspaceAdminDeps{
		Members: &fakeTeam{}, Invite: f.invite, Invites: fakeInvites{},
		Permissions:    fakeMemberPermissions{userAna: {{Resource: workspace.ResourceConversations, Action: workspace.ActionRead}}},
		SetPermissions: f.setPerms, Roles: fakeRoles{}, UpdateRole: f.updateRole, ScopedDepartments: f.departments,
	}
	return f
}

func as(userID string) copilot.Context {
	cc := member()
	cc.UserID = userID
	return cc
}

func adminTools(deps WorkspaceAdminDeps) []copilot.Tool {
	return []copilot.Tool{
		NewListWorkspaceMembersTool(deps), NewListWorkspaceInvitesTool(deps), NewGetMemberPermissionsTool(deps), NewListPermissionCatalogTool(deps),
		NewInviteMemberTool(deps), NewCancelInviteTool(deps), NewRemoveMemberTool(deps), NewChangeMemberRoleTool(deps), NewUpdateMemberPermissionsTool(deps),
		NewListRolesTool(deps), NewCreateRoleTool(deps), NewUpdateRoleTool(deps), NewDeleteRoleTool(deps),
		NewListDepartmentMembersTool(deps), NewCreateDepartmentTool(deps), NewUpdateDepartmentTool(deps), NewDeleteDepartmentTool(deps),
		NewAddDepartmentMemberTool(deps), NewRemoveDepartmentMemberTool(deps),
	}
}

func TestWorkspaceAdminToolsUseTheSamePermissionsAsTheScreens(t *testing.T) {
	want := map[string]string{
		"list_workspace_members": "members:read", "list_workspace_invites": "members:read", "get_member_permissions": "members:read",
		"list_permission_catalog": "ai_chat:read", "invite_member": "members:create", "cancel_invite": "members:delete",
		"remove_member": "members:delete", "change_member_role": "members:update", "update_member_permissions": "members:update",
		"list_roles": "roles:read", "create_role": "roles:create", "update_role": "roles:update", "delete_role": "roles:delete",
		"list_department_members": "departments:read", "create_department": "departments:create", "update_department": "departments:update",
		"delete_department": "departments:delete", "add_department_member": "departments:update", "remove_department_member": "departments:update",
	}
	for _, tool := range adminTools(WorkspaceAdminDeps{}) {
		m := tool.Meta()
		name := tool.Definition().Name
		if got := string(m.Resource) + ":" + string(m.Action); got != want[name] {
			t.Errorf("%s = %s, want %s", name, got, want[name])
		}
		if _, validates := tool.(copilot.Validator); m.Mutating && !validates {
			t.Errorf("%s changes the workspace without a preflight", name)
		}
	}
}

func TestInviteListNeverShowsTheInviteToken(t *testing.T) {
	res := NewListWorkspaceInvitesTool(newAdminFixture().deps).Execute(context.Background(), member(), nil)
	raw, _ := json.Marshal(res.Data)
	if strings.Contains(string(raw), "secret-token") {
		t.Fatalf("token leaked: %s", raw)
	}
}

func TestInviteMemberPreflight(t *testing.T) {
	f := newAdminFixture()
	tool := NewInviteMemberTool(f.deps)
	if err := validate(t, tool, as(userAna), map[string]interface{}{"email": "Novo@Loja.com", "department_ids": []interface{}{knownDept}}); err != nil {
		t.Fatalf("member inviting a member refused: %v", err)
	}
	cases := map[string]struct {
		cc   copilot.Context
		args map[string]interface{}
	}{
		"bad email":            {as(userAdmin), map[string]interface{}{"email": "não é email"}},
		"member invites admin": {as(userAna), map[string]interface{}{"email": "x@loja.com", "role": "admin"}},
		"unknown custom role":  {as(userAdmin), map[string]interface{}{"email": "x@loja.com", "role_id": "0b0b0b0b-0000-4000-8000-00000000ffff"}},
		"foreign department":   {as(userAdmin), map[string]interface{}{"email": "x@loja.com", "department_ids": []interface{}{foreignDept}}},
	}
	for label, c := range cases {
		if err := validate(t, tool, c.cc, c.args); !errors.Is(err, errInvalidArgs) {
			t.Errorf("%s: %v", label, err)
		}
	}
	res := tool.Execute(context.Background(), as(userAdmin), map[string]interface{}{"email": " Novo@Loja.com ", "workspace_id": "ws-2"})
	if res.Status != copilot.StatusOK || f.invite.in.Email != "novo@loja.com" || f.invite.in.Role != workspace.RoleMember {
		t.Fatalf("result %+v input %+v", res, f.invite.in)
	}
}

func TestMemberChangesFollowTheWorkspaceRules(t *testing.T) {
	deps := newAdminFixture().deps
	cases := map[string]struct {
		tool copilot.Tool
		cc   copilot.Context
		args map[string]interface{}
	}{
		"remove the owner":               {NewRemoveMemberTool(deps), as(userAdmin), map[string]interface{}{"user_id": userOwner}},
		"member removes an admin":        {NewRemoveMemberTool(deps), as(userAna), map[string]interface{}{"user_id": userAdmin}},
		"admin changes another admin":    {NewChangeMemberRoleTool(deps), as(userAdmin), map[string]interface{}{"user_id": userAdmin, "role": "member"}},
		"member changes a role":          {NewChangeMemberRoleTool(deps), as(userAna), map[string]interface{}{"user_id": userBia, "role": "admin"}},
		"role and role_id together":      {NewChangeMemberRoleTool(deps), as(userOwner), map[string]interface{}{"user_id": userBia, "role": "admin", "role_id": knownRole}},
		"invented user":                  {NewRemoveMemberTool(deps), as(userOwner), map[string]interface{}{"user_id": "ana"}},
		"someone from another workspace": {NewRemoveMemberTool(deps), as(userOwner), map[string]interface{}{"user_id": "0a0a0a0a-0000-4000-8000-0000000000ff"}},
		"edit own permissions":           {NewUpdateMemberPermissionsTool(deps), as(userAna), map[string]interface{}{"user_id": userAna, "grant": []interface{}{"agents:read"}}},
		"edit an admin's permissions":    {NewUpdateMemberPermissionsTool(deps), as(userOwner), map[string]interface{}{"user_id": userAdmin, "grant": []interface{}{"agents:read"}}},
		"invented permission":            {NewUpdateMemberPermissionsTool(deps), as(userOwner), map[string]interface{}{"user_id": userAna, "grant": []interface{}{"everything:all"}}},
	}
	for label, c := range cases {
		if err := validate(t, c.tool, c.cc, c.args); !errors.Is(err, errInvalidArgs) {
			t.Errorf("%s: %v", label, err)
		}
	}
	if err := validate(t, NewChangeMemberRoleTool(deps), as(userOwner), map[string]interface{}{"user_id": userAdmin, "role": "member"}); err != nil {
		t.Fatalf("owner demoting an admin refused: %v", err)
	}
}

func TestUpdateMemberPermissionsKeepsTheRestAndExplainsPrerequisites(t *testing.T) {
	f := newAdminFixture()
	tool := NewUpdateMemberPermissionsTool(f.deps)
	if err := validate(t, tool, as(userOwner), map[string]interface{}{"user_id": userAna, "grant": []interface{}{"agents:read_details"}}); err == nil || !strings.Contains(err.Error(), "precisa também de") {
		t.Fatalf("missing prerequisite passed: %v", err)
	}
	args := map[string]interface{}{"user_id": userAna, "grant": []interface{}{"agents:read"}, "revoke": []interface{}{"labels:read"}}
	fields := tool.(copilot.Describer).Describe(context.Background(), as(userOwner), args)
	if fieldValue(fields, "teamMember") != "Ana (ana@loja.com)" || fieldValue(fields, "grant") != "Visualizar agentes" {
		t.Fatalf("fields = %+v", fields)
	}
	res := tool.Execute(context.Background(), as(userOwner), args)
	if res.Status != copilot.StatusOK || f.setPerms.target != userAna {
		t.Fatalf("result %+v target %q", res, f.setPerms.target)
	}
	keys := entryKeys(f.setPerms.in.Permissions)
	if strings.Join(keys, ",") != "agents:read,conversations:read" {
		t.Fatalf("permissions sent = %v", keys)
	}
}

func TestUpdateRoleAppliesGrantAndRevokeToTheRolesPermissions(t *testing.T) {
	f := newAdminFixture()
	tool := NewUpdateRoleTool(f.deps)
	args := map[string]interface{}{"role_id": knownRole, "grant": []interface{}{"agents:read"}, "revoke": []interface{}{"labels:read"}}
	if err := validate(t, tool, as(userAdmin), args); err != nil {
		t.Fatalf("valid change refused: %v", err)
	}
	if err := validate(t, tool, as(userAna), args); !errors.Is(err, errInvalidArgs) {
		t.Fatalf("a plain member edited a role: %v", err)
	}
	if err := validate(t, tool, as(userAdmin), map[string]interface{}{"role_id": knownRole, "revoke": []interface{}{"labels:read", "conversations:read"}}); err == nil {
		t.Fatal("a role left with no permission passed")
	}
	if res := tool.Execute(context.Background(), as(userAdmin), args); res.Status != copilot.StatusOK {
		t.Fatalf("result = %+v", res)
	}
	if strings.Join(entryKeys(f.updateRole.in.Permissions), ",") != "agents:read,conversations:read" || f.updateRole.in.Name != nil {
		t.Fatalf("update input = %+v", f.updateRole.in)
	}
}

func TestDepartmentMembershipUsesTheWorkspaceMemberRecord(t *testing.T) {
	f := newAdminFixture()
	add := NewAddDepartmentMemberTool(f.deps)
	args := map[string]interface{}{"department_id": knownDept, "user_id": userAna}
	if err := validate(t, add, as(userAdmin), args); err != nil {
		t.Fatalf("valid add refused: %v", err)
	}
	if res := add.Execute(context.Background(), as(userAdmin), args); res.Status != copilot.StatusOK || len(f.departments.added) != 1 || f.departments.added[0] != "m-ana" {
		t.Fatalf("result %+v added %v", res, f.departments.added)
	}
	fields := add.(copilot.Describer).Describe(context.Background(), as(userAdmin), args)
	if fieldValue(fields, "teamMember") != "Ana (ana@loja.com)" || fieldValue(fields, "department") != "Vendas" {
		t.Fatalf("fields = %+v", fields)
	}
	if err := validate(t, NewRemoveDepartmentMemberTool(f.deps), as(userAdmin), map[string]interface{}{"department_id": foreignDept, "user_id": userAna}); !errors.Is(err, errInvalidArgs) {
		t.Fatalf("foreign department: %v", err)
	}
}

func TestApprovalCardsWarnAboutRiskyAccess(t *testing.T) {
	deps := newAdminFixture().deps
	risky := NewUpdateMemberPermissionsTool(deps).(copilot.Describer).Describe(context.Background(), as(userOwner),
		map[string]interface{}{"user_id": userAna, "grant": []interface{}{"whatsapp_campaigns:start", "labels:read"}})
	if got := fieldValue(risky, "risks"); !strings.Contains(got, "Iniciar envio de campanhas WhatsApp: Gera consumo do saldo do workspace.") || strings.Contains(got, "etiquetas") {
		t.Fatalf("risks = %q", got)
	}
	harmless := NewUpdateMemberPermissionsTool(deps).(copilot.Describer).Describe(context.Background(), as(userOwner),
		map[string]interface{}{"user_id": userAna, "grant": []interface{}{"labels:read"}})
	if fieldValue(harmless, "risks") != "" {
		t.Fatalf("a harmless grant was flagged: %+v", harmless)
	}
	promote := NewChangeMemberRoleTool(deps).(copilot.Describer).Describe(context.Background(), as(userOwner),
		map[string]interface{}{"user_id": userAna, "role": "admin"})
	if fieldValue(promote, "risks") != adminRiskSummary {
		t.Fatalf("promotion fields = %+v", promote)
	}
}

func TestPermissionReadsCarryTheirRiskKinds(t *testing.T) {
	deps := newAdminFixture().deps
	deps.Catalog = fakeCatalog{}
	res := NewListPermissionCatalogTool(deps).Execute(context.Background(), member(), map[string]interface{}{"search": "whatsapp_campaigns:start"})
	perms := res.Data.(map[string]interface{})["permissions"].([]map[string]interface{})
	if len(perms) != 1 || len(perms[0]["risks"].([]workspace.RiskKind)) != 2 {
		t.Fatalf("catalog = %+v", perms)
	}
}

type fakeCatalog struct{}

func (fakeCatalog) Execute() []workspace.ResourcePermissionInfo {
	return []workspace.ResourcePermissionInfo{{Resource: workspace.ResourceWhatsAppCampaigns, Actions: []workspace.Action{workspace.ActionRead, workspace.ActionStart}}}
}
