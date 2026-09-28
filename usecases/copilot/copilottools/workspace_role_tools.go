package copilottools

import (
	"context"
	"fmt"
	"strings"

	"vozko/domain/copilot"
	"vozko/domain/tools"
	"vozko/domain/workspace"
)

func rolesMeta(action workspace.Action, mutating bool) copilot.Meta {
	return copilot.Meta{Mutating: mutating, Resource: workspace.ResourceRoles, Action: action}
}

type listRolesTool struct{ deps WorkspaceAdminDeps }

func NewListRolesTool(deps WorkspaceAdminDeps) copilot.Tool { return &listRolesTool{deps: deps} }

func (t *listRolesTool) Meta() copilot.Meta { return rolesMeta(workspace.ActionRead, false) }

func (t *listRolesTool) Definition() tools.Definition {
	return definition("list_roles", "Lista as funções personalizadas do workspace com as permissões de cada uma.", struct{}{})
}

func (t *listRolesTool) Execute(_ context.Context, cc copilot.Context, _ map[string]interface{}) copilot.Result {
	roles, err := t.deps.Roles.Execute(cc.UserID, cc.WorkspaceID, platformRole(cc))
	if err != nil {
		return workspaceAdminFailure("list_roles", err)
	}
	out := make([]map[string]interface{}, 0, len(roles))
	for _, r := range roles {
		if r != nil {
			out = append(out, map[string]interface{}{"role_id": r.ID, "name": r.Name, "description": r.Description, "permissions": entryKeys(r.Permissions)})
		}
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{"roles": out}}
}

type createRoleArgs struct {
	Name        string   `json:"name" req:"true" desc:"nome da função (ex.: Atendente)"`
	Description string   `json:"description" desc:"para que serve a função"`
	Permissions []string `json:"permissions" req:"true" desc:"permissões no formato recurso:ação de list_permission_catalog"`
}

type createRoleTool struct{ deps WorkspaceAdminDeps }

func NewCreateRoleTool(deps WorkspaceAdminDeps) copilot.Tool { return &createRoleTool{deps: deps} }

func (t *createRoleTool) Meta() copilot.Meta { return rolesMeta(workspace.ActionCreate, true) }

func (t *createRoleTool) Definition() tools.Definition {
	return definition("create_role", "Cria uma função personalizada com um conjunto de permissões. Só depois da aprovação do usuário.", createRoleArgs{})
}

func (t *createRoleTool) Validate(_ context.Context, cc copilot.Context, args map[string]interface{}) error {
	a, err := validateArgs[createRoleArgs](nil, cc, args)
	if err != nil {
		return err
	}
	if err := t.deps.requireManager(cc, "create_role"); err != nil {
		return err
	}
	perms, err := parsePermissions(a.Permissions)
	if err != nil {
		return err
	}
	return requireCompletePermissions(perms)
}

func (t *createRoleTool) Describe(_ context.Context, _ copilot.Context, args map[string]interface{}) []copilot.Field {
	var a createRoleArgs
	bindArgs(args, &a)
	fields := []copilot.Field{{Key: "name", Value: strings.TrimSpace(a.Name)}}
	if perms, err := parsePermissions(a.Permissions); err == nil {
		fields = append(fields, copilot.Field{Key: "permissions", Value: permissionLabels(perms)})
		fields = withRisks(fields, riskSummary(perms))
	}
	return fields
}

func (t *createRoleTool) Execute(_ context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a createRoleArgs
	if err := decodeArgs(args, &a); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	perms, err := parsePermissions(a.Permissions)
	if err != nil {
		return workspaceAdminFailure("create_role", err)
	}
	role, err := t.deps.CreateRole.Execute(cc.UserID, cc.WorkspaceID, platformRole(cc), workspace.CreateCustomRoleInput{
		Name: strings.TrimSpace(a.Name), Description: strings.TrimSpace(a.Description), Permissions: perms,
	})
	if err != nil {
		return workspaceAdminFailure("create_role", err)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{"role_id": role.ID, "name": role.Name}}
}

type updateRoleArgs struct {
	RoleID      string   `json:"role_id" req:"true" id:"true" desc:"role_id exato de list_roles"`
	Name        string   `json:"name" desc:"novo nome, se mudar"`
	Description string   `json:"description" desc:"nova descrição, se mudar"`
	Grant       []string `json:"grant" desc:"permissões a acrescentar, no formato recurso:ação"`
	Revoke      []string `json:"revoke" desc:"permissões a tirar, no formato recurso:ação"`
}

type roleChange struct {
	role   *workspace.CustomRole
	grant  []workspace.PermissionEntry
	revoke []workspace.PermissionEntry
	input  workspace.UpdateCustomRoleInput
}

type updateRoleTool struct{ deps WorkspaceAdminDeps }

func NewUpdateRoleTool(deps WorkspaceAdminDeps) copilot.Tool { return &updateRoleTool{deps: deps} }

func (t *updateRoleTool) Meta() copilot.Meta { return rolesMeta(workspace.ActionUpdate, true) }

func (t *updateRoleTool) Definition() tools.Definition {
	return definition("update_role",
		"Renomeia uma função personalizada ou dá e tira permissões dela. Todos os membros com essa função recebem a mudança. "+
			"Só depois da aprovação do usuário.", updateRoleArgs{})
}

func (t *updateRoleTool) change(cc copilot.Context, a updateRoleArgs) (roleChange, error) {
	var c roleChange
	var err error
	if c.role, err = t.deps.role(cc, a.RoleID); err != nil {
		return c, err
	}
	if c.grant, err = parsePermissions(a.Grant); err != nil {
		return c, err
	}
	if c.revoke, err = parsePermissions(a.Revoke); err != nil {
		return c, err
	}
	if name := strings.TrimSpace(a.Name); name != "" {
		c.input.Name = &name
	}
	if description := strings.TrimSpace(a.Description); description != "" {
		c.input.Description = &description
	}
	if len(c.grant) > 0 || len(c.revoke) > 0 {
		c.input.Permissions = workspace.ApplyPermissionChanges(c.role.Permissions, c.grant, c.revoke)
	}
	if c.input.Name == nil && c.input.Description == nil && c.input.Permissions == nil {
		return c, fmt.Errorf("%w: informe o que muda (name, description, grant ou revoke)", errInvalidArgs)
	}
	return c, nil
}

func (t *updateRoleTool) Validate(_ context.Context, cc copilot.Context, args map[string]interface{}) error {
	a, err := validateArgs[updateRoleArgs](nil, cc, args)
	if err != nil {
		return err
	}
	if err := t.deps.requireManager(cc, "update_role"); err != nil {
		return err
	}
	c, err := t.change(cc, a)
	if err != nil {
		return err
	}
	if c.input.Permissions != nil {
		if len(c.input.Permissions) == 0 {
			return fmt.Errorf("%w: a função ficaria sem nenhuma permissão", errInvalidArgs)
		}
		return requireCompletePermissions(c.input.Permissions)
	}
	return nil
}

func (t *updateRoleTool) Describe(_ context.Context, cc copilot.Context, args map[string]interface{}) []copilot.Field {
	var a updateRoleArgs
	bindArgs(args, &a)
	c, err := t.change(cc, a)
	if err != nil {
		return []copilot.Field{{Key: "role", Value: "função desconhecida"}}
	}
	fields := []copilot.Field{{Key: "role", Value: c.role.Name}}
	if c.input.Name != nil {
		fields = append(fields, copilot.Field{Key: "newName", Value: *c.input.Name})
	}
	if len(c.grant) > 0 {
		fields = append(fields, copilot.Field{Key: "grant", Value: permissionLabels(c.grant)})
	}
	if len(c.revoke) > 0 {
		fields = append(fields, copilot.Field{Key: "revoke", Value: permissionLabels(c.revoke)})
	}
	return withRisks(fields, riskSummary(c.grant))
}

func (t *updateRoleTool) Execute(_ context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a updateRoleArgs
	if err := decodeArgs(args, &a); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	c, err := t.change(cc, a)
	if err != nil {
		return workspaceAdminFailure("update_role", err)
	}
	role, err := t.deps.UpdateRole.Execute(cc.UserID, cc.WorkspaceID, platformRole(cc), c.role.ID, c.input)
	if err != nil {
		return workspaceAdminFailure("update_role", err)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{"role_id": role.ID, "name": role.Name, "permissions": entryKeys(role.Permissions)}}
}

type roleRefArgs struct {
	RoleID string `json:"role_id" req:"true" id:"true" desc:"role_id exato de list_roles"`
}

type deleteRoleTool struct{ deps WorkspaceAdminDeps }

func NewDeleteRoleTool(deps WorkspaceAdminDeps) copilot.Tool { return &deleteRoleTool{deps: deps} }

func (t *deleteRoleTool) Meta() copilot.Meta { return rolesMeta(workspace.ActionDelete, true) }

func (t *deleteRoleTool) Definition() tools.Definition {
	return definition("delete_role",
		"Exclui uma função personalizada que não está atribuída a ninguém. Só depois da aprovação do usuário.", roleRefArgs{})
}

func (t *deleteRoleTool) Validate(_ context.Context, cc copilot.Context, args map[string]interface{}) error {
	a, err := validateArgs[roleRefArgs](nil, cc, args)
	if err != nil {
		return err
	}
	if err := t.deps.requireManager(cc, "delete_role"); err != nil {
		return err
	}
	_, err = t.deps.role(cc, a.RoleID)
	return err
}

func (t *deleteRoleTool) Describe(_ context.Context, cc copilot.Context, args map[string]interface{}) []copilot.Field {
	var a roleRefArgs
	bindArgs(args, &a)
	if role, err := t.deps.role(cc, a.RoleID); err == nil {
		return []copilot.Field{{Key: "role", Value: role.Name}}
	}
	return []copilot.Field{{Key: "role", Value: "função desconhecida"}}
}

func (t *deleteRoleTool) Execute(_ context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a roleRefArgs
	if err := decodeArgs(args, &a); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	if err := t.deps.DeleteRole.Execute(cc.UserID, cc.WorkspaceID, platformRole(cc), strings.TrimSpace(a.RoleID)); err != nil {
		return workspaceAdminFailure("delete_role", err)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{"deleted": true}}
}

func (d WorkspaceAdminDeps) requireManager(cc copilot.Context, tool string) error {
	actor, err := d.actor(cc)
	if err == nil {
		err = actor.ManagesMembers()
	}
	if err != nil {
		return fmt.Errorf("%w: %s", errInvalidArgs, workspaceAdminFailure(tool, err).Message)
	}
	return nil
}
