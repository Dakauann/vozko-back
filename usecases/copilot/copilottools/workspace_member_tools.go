package copilottools

import (
	"context"
	"fmt"
	"net/mail"
	"strings"

	"vozko/domain/copilot"
	"vozko/domain/tools"
	"vozko/domain/workspace"
)

func membersMeta(action workspace.Action, mutating bool) copilot.Meta {
	return copilot.Meta{Mutating: mutating, Resource: workspace.ResourceMembers, Action: action}
}

type listWorkspaceMembersTool struct{ deps WorkspaceAdminDeps }

func NewListWorkspaceMembersTool(deps WorkspaceAdminDeps) copilot.Tool {
	return &listWorkspaceMembersTool{deps: deps}
}

func (t *listWorkspaceMembersTool) Meta() copilot.Meta { return membersMeta(workspace.ActionRead, false) }

func (t *listWorkspaceMembersTool) Definition() tools.Definition {
	return definition("list_workspace_members",
		"Lista os membros do workspace com a função de cada um (dono, administrador, membro ou função personalizada). "+
			"Use o user_id nas ferramentas de gestão da equipe.", struct{}{})
}

func (t *listWorkspaceMembersTool) Execute(_ context.Context, cc copilot.Context, _ map[string]interface{}) copilot.Result {
	members, err := t.deps.members(cc)
	if err != nil {
		return workspaceAdminFailure("list_workspace_members", err)
	}
	out := make([]map[string]interface{}, 0, len(members))
	for _, m := range members {
		if m != nil {
			out = append(out, map[string]interface{}{
				"user_id": m.UserID, "name": m.Username, "email": m.Email, "role": m.Role, "custom_role": m.RoleName,
			})
		}
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{"members": out}}
}

type listWorkspaceInvitesTool struct{ deps WorkspaceAdminDeps }

func NewListWorkspaceInvitesTool(deps WorkspaceAdminDeps) copilot.Tool {
	return &listWorkspaceInvitesTool{deps: deps}
}

func (t *listWorkspaceInvitesTool) Meta() copilot.Meta { return membersMeta(workspace.ActionRead, false) }

func (t *listWorkspaceInvitesTool) Definition() tools.Definition {
	return definition("list_workspace_invites", "Lista os convites do workspace (e-mail, função e status).", struct{}{})
}

func (t *listWorkspaceInvitesTool) Execute(_ context.Context, cc copilot.Context, _ map[string]interface{}) copilot.Result {
	invites, err := t.deps.Invites.Execute(cc.UserID, cc.WorkspaceID, platformRole(cc))
	if err != nil {
		return workspaceAdminFailure("list_workspace_invites", err)
	}
	out := make([]map[string]interface{}, 0, len(invites))
	for _, i := range invites {
		if i != nil {
			out = append(out, map[string]interface{}{"invite_id": i.ID, "email": i.Email, "role": i.Role, "role_id": i.RoleID, "status": i.Status})
		}
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{"invites": out}}
}

type memberRefArgs struct {
	UserID string `json:"user_id" req:"true" id:"true" desc:"user_id exato de list_workspace_members"`
}

type getMemberPermissionsTool struct{ deps WorkspaceAdminDeps }

func NewGetMemberPermissionsTool(deps WorkspaceAdminDeps) copilot.Tool {
	return &getMemberPermissionsTool{deps: deps}
}

func (t *getMemberPermissionsTool) Meta() copilot.Meta { return membersMeta(workspace.ActionRead, false) }

func (t *getMemberPermissionsTool) Definition() tools.Definition {
	return definition("get_member_permissions", "Mostra as permissões de um membro, com a descrição de cada uma.", memberRefArgs{})
}

func (t *getMemberPermissionsTool) Execute(_ context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a memberRefArgs
	if err := decodeArgs(args, &a); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	target, err := t.deps.member(cc, a.UserID)
	if err != nil {
		return workspaceAdminFailure("get_member_permissions", err)
	}
	if target.Role == workspace.RoleOwner || target.Role == workspace.RoleAdmin {
		return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{"member": memberLabel(target), "role": target.Role, "all_permissions": true}}
	}
	perms, err := t.deps.Permissions.Execute(cc.UserID, cc.WorkspaceID, target.UserID, platformRole(cc))
	if err != nil {
		return workspaceAdminFailure("get_member_permissions", err)
	}
	out := make([]map[string]interface{}, 0, len(perms))
	for _, p := range perms {
		if p != nil {
			entry := p.Entry()
			out = append(out, map[string]interface{}{"permission": entry.Key(), "description": workspace.DescribePermission(entry), "risks": riskKinds(entry)})
		}
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{"member": memberLabel(target), "role": roleLabel(target), "permissions": out}}
}

type listPermissionCatalogArgs struct {
	Search string `json:"search" desc:"filtra por texto (ex.: campanhas, agentes); omita para ver tudo"`
}

type listPermissionCatalogTool struct{ deps WorkspaceAdminDeps }

func NewListPermissionCatalogTool(deps WorkspaceAdminDeps) copilot.Tool {
	return &listPermissionCatalogTool{deps: deps}
}

func (t *listPermissionCatalogTool) Meta() copilot.Meta {
	return copilot.Meta{Resource: workspace.ResourceAIChat, Action: workspace.ActionRead}
}

func (t *listPermissionCatalogTool) Definition() tools.Definition {
	return definition("list_permission_catalog",
		"Lista as permissões que existem no produto no formato recurso:ação, com a descrição de cada uma. Use antes de "+
			"alterar permissões de um membro ou de uma função.", listPermissionCatalogArgs{})
}

func (t *listPermissionCatalogTool) Execute(_ context.Context, _ copilot.Context, args map[string]interface{}) copilot.Result {
	var a listPermissionCatalogArgs
	bindArgs(args, &a)
	search := strings.ToLower(strings.TrimSpace(a.Search))
	out := make([]map[string]interface{}, 0)
	for _, info := range t.deps.Catalog.Execute() {
		for _, action := range info.Actions {
			entry := workspace.PermissionEntry{Resource: info.Resource, Action: action}
			description := workspace.DescribePermission(entry)
			if search != "" && !strings.Contains(strings.ToLower(entry.Key()+" "+description), search) {
				continue
			}
			out = append(out, map[string]interface{}{"permission": entry.Key(), "description": description, "risks": riskKinds(entry)})
		}
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{"permissions": out}}
}

type inviteMemberArgs struct {
	Email         string   `json:"email" req:"true" desc:"e-mail da pessoa convidada"`
	Role          string   `json:"role" enum:"admin,member" desc:"admin ou member; omita quando usar role_id"`
	RoleID        string   `json:"role_id" id:"true" desc:"função personalizada de list_roles"`
	DepartmentIDs []string `json:"department_ids" id:"true" desc:"departamentos de list_departments em que a pessoa vai entrar"`
}

func (a inviteMemberArgs) input() workspace.InviteMemberInput {
	role := workspace.Role(strings.TrimSpace(a.Role))
	if role == "" && strings.TrimSpace(a.RoleID) == "" {
		role = workspace.RoleMember
	}
	return workspace.InviteMemberInput{
		Email: strings.ToLower(strings.TrimSpace(a.Email)), Role: role, RoleID: strings.TrimSpace(a.RoleID), DepartmentIDs: a.DepartmentIDs,
	}
}

type inviteMemberTool struct{ deps WorkspaceAdminDeps }

func NewInviteMemberTool(deps WorkspaceAdminDeps) copilot.Tool { return &inviteMemberTool{deps: deps} }

func (t *inviteMemberTool) Meta() copilot.Meta { return membersMeta(workspace.ActionCreate, true) }

func (t *inviteMemberTool) Definition() tools.Definition {
	return definition("invite_member",
		"Convida uma pessoa por e-mail para o workspace, com uma função e, opcionalmente, departamentos. Só depois da aprovação do usuário.",
		inviteMemberArgs{})
}

func (t *inviteMemberTool) Validate(_ context.Context, cc copilot.Context, args map[string]interface{}) error {
	a, err := validateArgs[inviteMemberArgs](nil, cc, args)
	if err != nil {
		return err
	}
	in := a.input()
	if _, err := mail.ParseAddress(in.Email); err != nil {
		return fmt.Errorf("%w: e-mail inválido", errInvalidArgs)
	}
	actor, err := t.deps.actor(cc)
	if err != nil {
		return err
	}
	if err := actor.CanInviteAs(in.Role); err != nil {
		return fmt.Errorf("%w: %s", errInvalidArgs, workspaceAdminFailure("invite_member", err).Message)
	}
	if in.RoleID != "" {
		if _, err := t.deps.role(cc, in.RoleID); err != nil {
			return err
		}
	}
	for _, id := range in.DepartmentIDs {
		if _, err := t.deps.department(cc, id); err != nil {
			return err
		}
	}
	return nil
}

func (t *inviteMemberTool) Describe(_ context.Context, cc copilot.Context, args map[string]interface{}) []copilot.Field {
	var a inviteMemberArgs
	bindArgs(args, &a)
	in := a.input()
	fields := []copilot.Field{{Key: "email", Value: in.Email}}
	role := string(in.Role)
	if r, err := t.deps.role(cc, in.RoleID); in.RoleID != "" && err == nil {
		role = r.Name
	}
	fields = append(fields, copilot.Field{Key: "role", Value: role})
	if names := t.departmentNames(cc, in.DepartmentIDs); names != "" {
		fields = append(fields, copilot.Field{Key: "departments", Value: names})
	}
	return withRisks(fields, t.deps.grantedRisks(cc, in.Role, in.RoleID))
}

func (t *inviteMemberTool) departmentNames(cc copilot.Context, ids []string) string {
	names := make([]string, 0, len(ids))
	for _, id := range ids {
		if d, err := t.deps.department(cc, id); err == nil {
			names = append(names, d.Name)
		}
	}
	return strings.Join(names, ", ")
}

func (t *inviteMemberTool) Execute(_ context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a inviteMemberArgs
	if err := decodeArgs(args, &a); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	invite, err := t.deps.Invite.Execute(cc.UserID, cc.WorkspaceID, platformRole(cc), a.input())
	if err != nil {
		return workspaceAdminFailure("invite_member", err)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{"invited": invite.Email, "invite_id": invite.ID}}
}

type cancelInviteArgs struct {
	InviteID string `json:"invite_id" req:"true" id:"true" desc:"invite_id exato de list_workspace_invites"`
}

type cancelInviteTool struct{ deps WorkspaceAdminDeps }

func NewCancelInviteTool(deps WorkspaceAdminDeps) copilot.Tool { return &cancelInviteTool{deps: deps} }

func (t *cancelInviteTool) Meta() copilot.Meta { return membersMeta(workspace.ActionDelete, true) }

func (t *cancelInviteTool) Definition() tools.Definition {
	return definition("cancel_invite", "Cancela um convite pendente. Só depois da aprovação do usuário.", cancelInviteArgs{})
}

func (t *cancelInviteTool) pending(cc copilot.Context, raw string) (*workspace.Invite, error) {
	invites, err := t.deps.Invites.Execute(cc.UserID, cc.WorkspaceID, platformRole(cc))
	if err != nil {
		return nil, err
	}
	for _, i := range invites {
		if i != nil && i.ID == strings.TrimSpace(raw) && i.Status == workspace.InviteStatusPending {
			return i, nil
		}
	}
	return nil, fmt.Errorf("%w: convite pendente não encontrado; use list_workspace_invites", errInvalidArgs)
}

func (t *cancelInviteTool) Validate(_ context.Context, cc copilot.Context, args map[string]interface{}) error {
	a, err := validateArgs[cancelInviteArgs](nil, cc, args)
	if err != nil {
		return err
	}
	_, err = t.pending(cc, a.InviteID)
	return err
}

func (t *cancelInviteTool) Describe(_ context.Context, cc copilot.Context, args map[string]interface{}) []copilot.Field {
	var a cancelInviteArgs
	bindArgs(args, &a)
	if invite, err := t.pending(cc, a.InviteID); err == nil {
		return []copilot.Field{{Key: "email", Value: invite.Email}}
	}
	return []copilot.Field{{Key: "email", Value: "convite desconhecido"}}
}

func (t *cancelInviteTool) Execute(_ context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a cancelInviteArgs
	if err := decodeArgs(args, &a); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	if err := t.deps.CancelInvite.Execute(cc.UserID, cc.WorkspaceID, platformRole(cc), strings.TrimSpace(a.InviteID)); err != nil {
		return workspaceAdminFailure("cancel_invite", err)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{"cancelled": true}}
}

type removeMemberTool struct{ deps WorkspaceAdminDeps }

func NewRemoveMemberTool(deps WorkspaceAdminDeps) copilot.Tool { return &removeMemberTool{deps: deps} }

func (t *removeMemberTool) Meta() copilot.Meta { return membersMeta(workspace.ActionDelete, true) }

func (t *removeMemberTool) Definition() tools.Definition {
	return definition("remove_member",
		"Remove uma pessoa do workspace: ela perde o acesso na hora. Só depois da aprovação do usuário.", memberRefArgs{})
}

func (t *removeMemberTool) Validate(_ context.Context, cc copilot.Context, args map[string]interface{}) error {
	a, err := validateArgs[memberRefArgs](nil, cc, args)
	if err != nil {
		return err
	}
	target, err := t.deps.member(cc, a.UserID)
	if err != nil {
		return err
	}
	actor, err := t.deps.actor(cc)
	if err != nil {
		return err
	}
	if err := actor.CanRemove(target); err != nil {
		return fmt.Errorf("%w: %s", errInvalidArgs, workspaceAdminFailure("remove_member", err).Message)
	}
	return nil
}

func (t *removeMemberTool) Describe(_ context.Context, cc copilot.Context, args map[string]interface{}) []copilot.Field {
	return describeMember(t.deps, cc, args)
}

func describeMember(deps WorkspaceAdminDeps, cc copilot.Context, args map[string]interface{}) []copilot.Field {
	var a memberRefArgs
	bindArgs(args, &a)
	target, err := deps.member(cc, a.UserID)
	if err != nil {
		return []copilot.Field{{Key: "teamMember", Value: "membro desconhecido"}}
	}
	return []copilot.Field{{Key: "teamMember", Value: memberLabel(target)}, {Key: "currentRole", Value: roleLabel(target)}}
}

func (t *removeMemberTool) Execute(_ context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a memberRefArgs
	if err := decodeArgs(args, &a); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	if err := t.deps.RemoveMember.Execute(cc.UserID, cc.WorkspaceID, strings.TrimSpace(a.UserID), platformRole(cc)); err != nil {
		return workspaceAdminFailure("remove_member", err)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{"removed": true}}
}

type changeMemberRoleArgs struct {
	memberRefArgs
	Role   string `json:"role" enum:"admin,member" desc:"nova função padrão: admin ou member"`
	RoleID string `json:"role_id" id:"true" desc:"ou uma função personalizada de list_roles"`
}

type changeMemberRoleTool struct{ deps WorkspaceAdminDeps }

func NewChangeMemberRoleTool(deps WorkspaceAdminDeps) copilot.Tool {
	return &changeMemberRoleTool{deps: deps}
}

func (t *changeMemberRoleTool) Meta() copilot.Meta { return membersMeta(workspace.ActionUpdate, true) }

func (t *changeMemberRoleTool) Definition() tools.Definition {
	return definition("change_member_role",
		"Troca a função de um membro: admin, member ou uma função personalizada (role_id). Uma função personalizada substitui "+
			"as permissões do membro pelas da função. Só depois da aprovação do usuário.", changeMemberRoleArgs{})
}

func (t *changeMemberRoleTool) Validate(_ context.Context, cc copilot.Context, args map[string]interface{}) error {
	a, err := validateArgs[changeMemberRoleArgs](nil, cc, args)
	if err != nil {
		return err
	}
	if (strings.TrimSpace(a.Role) == "") == (strings.TrimSpace(a.RoleID) == "") {
		return fmt.Errorf("%w: informe role ou role_id, um dos dois", errInvalidArgs)
	}
	target, err := t.deps.member(cc, a.UserID)
	if err != nil {
		return err
	}
	actor, err := t.deps.actor(cc)
	if err != nil {
		return err
	}
	if err := actor.ManagesMembers(); err != nil {
		return fmt.Errorf("%w: %s", errInvalidArgs, workspaceAdminFailure("change_member_role", err).Message)
	}
	if err := actor.CanReassign(target); err != nil {
		return fmt.Errorf("%w: %s", errInvalidArgs, workspaceAdminFailure("change_member_role", err).Message)
	}
	if a.RoleID != "" {
		_, err = t.deps.role(cc, a.RoleID)
	}
	return err
}

func (t *changeMemberRoleTool) Describe(_ context.Context, cc copilot.Context, args map[string]interface{}) []copilot.Field {
	var a changeMemberRoleArgs
	bindArgs(args, &a)
	fields := describeMember(t.deps, cc, args)
	next := strings.TrimSpace(a.Role)
	if r, err := t.deps.role(cc, a.RoleID); strings.TrimSpace(a.RoleID) != "" && err == nil {
		next = r.Name
	}
	fields = append(fields, copilot.Field{Key: "newRole", Value: next})
	return withRisks(fields, t.deps.grantedRisks(cc, workspace.Role(strings.TrimSpace(a.Role)), a.RoleID))
}

func (t *changeMemberRoleTool) Execute(_ context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a changeMemberRoleArgs
	if err := decodeArgs(args, &a); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	userID := strings.TrimSpace(a.UserID)
	var member *workspace.Member
	var err error
	if roleID := strings.TrimSpace(a.RoleID); roleID != "" {
		member, err = t.deps.AssignRole.Execute(cc.UserID, cc.WorkspaceID, userID, platformRole(cc), roleID)
	} else {
		member, err = t.deps.ChangeRole.Execute(cc.UserID, cc.WorkspaceID, userID, platformRole(cc), workspace.Role(strings.TrimSpace(a.Role)))
	}
	if err != nil {
		return workspaceAdminFailure("change_member_role", err)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{"member": memberLabel(member), "role": roleLabel(member)}}
}

type updateMemberPermissionsArgs struct {
	memberRefArgs
	Grant  []string `json:"grant" desc:"permissões a dar, no formato recurso:ação de list_permission_catalog"`
	Revoke []string `json:"revoke" desc:"permissões a tirar, no formato recurso:ação"`
}

type memberPermissionChange struct {
	target  *workspace.Member
	grant   []workspace.PermissionEntry
	revoke  []workspace.PermissionEntry
	current []workspace.PermissionEntry
}

func (c memberPermissionChange) result() []workspace.PermissionEntry {
	return workspace.ApplyPermissionChanges(c.current, c.grant, c.revoke)
}

type updateMemberPermissionsTool struct{ deps WorkspaceAdminDeps }

func NewUpdateMemberPermissionsTool(deps WorkspaceAdminDeps) copilot.Tool {
	return &updateMemberPermissionsTool{deps: deps}
}

func (t *updateMemberPermissionsTool) Meta() copilot.Meta { return membersMeta(workspace.ActionUpdate, true) }

func (t *updateMemberPermissionsTool) Definition() tools.Definition {
	return definition("update_member_permissions",
		"Dá ou tira permissões de um membro (não vale para dono e administradores, que já têm todas). Mantém as demais "+
			"permissões como estão. Só depois da aprovação do usuário.", updateMemberPermissionsArgs{})
}

func (t *updateMemberPermissionsTool) change(cc copilot.Context, a updateMemberPermissionsArgs) (memberPermissionChange, error) {
	var c memberPermissionChange
	var err error
	if len(a.Grant) == 0 && len(a.Revoke) == 0 {
		return c, fmt.Errorf("%w: informe grant ou revoke", errInvalidArgs)
	}
	if c.grant, err = parsePermissions(a.Grant); err != nil {
		return c, err
	}
	if c.revoke, err = parsePermissions(a.Revoke); err != nil {
		return c, err
	}
	if c.target, err = t.deps.member(cc, a.UserID); err != nil {
		return c, err
	}
	if err := workspace.CanEditPermissions(cc.UserID, c.target); err != nil {
		return c, fmt.Errorf("%w: %s", errInvalidArgs, workspaceAdminFailure("update_member_permissions", err).Message)
	}
	perms, err := t.deps.Permissions.Execute(cc.UserID, cc.WorkspaceID, c.target.UserID, platformRole(cc))
	if err != nil {
		return c, err
	}
	c.current = workspace.EntriesOf(perms)
	return c, nil
}

func (t *updateMemberPermissionsTool) Validate(_ context.Context, cc copilot.Context, args map[string]interface{}) error {
	a, err := validateArgs[updateMemberPermissionsArgs](nil, cc, args)
	if err != nil {
		return err
	}
	c, err := t.change(cc, a)
	if err != nil {
		return err
	}
	return requireCompletePermissions(c.result())
}

func (t *updateMemberPermissionsTool) Describe(_ context.Context, cc copilot.Context, args map[string]interface{}) []copilot.Field {
	var a updateMemberPermissionsArgs
	bindArgs(args, &a)
	c, err := t.change(cc, a)
	if err != nil {
		return []copilot.Field{{Key: "teamMember", Value: "membro desconhecido"}}
	}
	fields := []copilot.Field{{Key: "teamMember", Value: memberLabel(c.target)}}
	if len(c.grant) > 0 {
		fields = append(fields, copilot.Field{Key: "grant", Value: permissionLabels(c.grant)})
	}
	if len(c.revoke) > 0 {
		fields = append(fields, copilot.Field{Key: "revoke", Value: permissionLabels(c.revoke)})
	}
	return withRisks(fields, riskSummary(c.grant))
}

func (t *updateMemberPermissionsTool) Execute(_ context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a updateMemberPermissionsArgs
	if err := decodeArgs(args, &a); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	c, err := t.change(cc, a)
	if err != nil {
		return workspaceAdminFailure("update_member_permissions", err)
	}
	saved, err := t.deps.SetPermissions.Execute(cc.UserID, cc.WorkspaceID, c.target.UserID, platformRole(cc), workspace.SetPermissionsInput{Permissions: c.result()})
	if err != nil {
		return workspaceAdminFailure("update_member_permissions", err)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{"member": memberLabel(c.target), "permissions": entryKeys(workspace.EntriesOf(saved))}}
}
