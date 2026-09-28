package copilottools

import (
	"context"
	"fmt"
	"strings"

	"vozko/domain/copilot"
	"vozko/domain/tools"
	"vozko/domain/workspace"
	wd "vozko/domain/workspace/workspace_department"
)

func departmentsMeta(action workspace.Action, mutating bool) copilot.Meta {
	return copilot.Meta{Mutating: mutating, Resource: workspace.ResourceDepartments, Action: action}
}

type departmentRefArgs struct {
	DepartmentID string `json:"department_id" req:"true" id:"true" desc:"id exato de list_departments"`
}

func describeDepartment(deps WorkspaceAdminDeps, cc copilot.Context, id string) copilot.Field {
	if d, err := deps.department(cc, id); err == nil {
		return copilot.Field{Key: "department", Value: d.Name}
	}
	return copilot.Field{Key: "department", Value: "departamento desconhecido"}
}

type listDepartmentMembersTool struct{ deps WorkspaceAdminDeps }

func NewListDepartmentMembersTool(deps WorkspaceAdminDeps) copilot.Tool {
	return &listDepartmentMembersTool{deps: deps}
}

func (t *listDepartmentMembersTool) Meta() copilot.Meta { return departmentsMeta(workspace.ActionRead, false) }

func (t *listDepartmentMembersTool) Definition() tools.Definition {
	return definition("list_department_members", "Lista quem faz parte de um departamento.", departmentRefArgs{})
}

func (t *listDepartmentMembersTool) Execute(_ context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a departmentRefArgs
	if err := decodeArgs(args, &a); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	members, err := t.deps.ScopedDepartments.ListMembers(cc.WorkspaceID, strings.TrimSpace(a.DepartmentID))
	if err != nil {
		return workspaceAdminFailure("list_department_members", err)
	}
	out := make([]map[string]interface{}, 0, len(members))
	for _, m := range members {
		out = append(out, map[string]interface{}{"user_id": m.UserID, "name": m.Username, "email": m.Email, "role": m.Role})
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{"members": out}}
}

type createDepartmentArgs struct {
	Name        string `json:"name" req:"true" desc:"nome do departamento"`
	Description string `json:"description" desc:"do que o departamento cuida"`
}

type createDepartmentTool struct{ deps WorkspaceAdminDeps }

func NewCreateDepartmentTool(deps WorkspaceAdminDeps) copilot.Tool {
	return &createDepartmentTool{deps: deps}
}

func (t *createDepartmentTool) Meta() copilot.Meta { return departmentsMeta(workspace.ActionCreate, true) }

func (t *createDepartmentTool) Definition() tools.Definition {
	return definition("create_department", "Cria um departamento. Só depois da aprovação do usuário.", createDepartmentArgs{})
}

func (t *createDepartmentTool) Validate(_ context.Context, cc copilot.Context, args map[string]interface{}) error {
	_, err := validateArgs[createDepartmentArgs](nil, cc, args)
	return err
}

func (t *createDepartmentTool) Describe(_ context.Context, _ copilot.Context, args map[string]interface{}) []copilot.Field {
	var a createDepartmentArgs
	bindArgs(args, &a)
	return []copilot.Field{{Key: "name", Value: strings.TrimSpace(a.Name)}, {Key: "description", Value: strings.TrimSpace(a.Description)}}
}

func (t *createDepartmentTool) Execute(_ context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a createDepartmentArgs
	if err := decodeArgs(args, &a); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	d, err := t.deps.CreateDepartment.Execute(wd.CreateDepartmentInput{WorkspaceID: cc.WorkspaceID, Name: strings.TrimSpace(a.Name), Description: strings.TrimSpace(a.Description)})
	if err != nil {
		return workspaceAdminFailure("create_department", err)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{"department_id": d.ID, "name": d.Name}}
}

type updateDepartmentArgs struct {
	departmentRefArgs
	Name        string `json:"name" desc:"novo nome, se mudar"`
	Description string `json:"description" desc:"nova descrição, se mudar"`
}

func (a updateDepartmentArgs) input() (wd.UpdateDepartmentInput, error) {
	var in wd.UpdateDepartmentInput
	if name := strings.TrimSpace(a.Name); name != "" {
		in.Name = &name
	}
	if description := strings.TrimSpace(a.Description); description != "" {
		in.Description = &description
	}
	if in.Name == nil && in.Description == nil {
		return in, fmt.Errorf("%w: informe name ou description", errInvalidArgs)
	}
	return in, nil
}

type updateDepartmentTool struct{ deps WorkspaceAdminDeps }

func NewUpdateDepartmentTool(deps WorkspaceAdminDeps) copilot.Tool {
	return &updateDepartmentTool{deps: deps}
}

func (t *updateDepartmentTool) Meta() copilot.Meta { return departmentsMeta(workspace.ActionUpdate, true) }

func (t *updateDepartmentTool) Definition() tools.Definition {
	return definition("update_department", "Renomeia um departamento ou muda a descrição. Só depois da aprovação do usuário.", updateDepartmentArgs{})
}

func (t *updateDepartmentTool) Validate(_ context.Context, cc copilot.Context, args map[string]interface{}) error {
	a, err := validateArgs[updateDepartmentArgs](nil, cc, args)
	if err != nil {
		return err
	}
	if _, err := a.input(); err != nil {
		return err
	}
	_, err = t.deps.department(cc, a.DepartmentID)
	return err
}

func (t *updateDepartmentTool) Describe(_ context.Context, cc copilot.Context, args map[string]interface{}) []copilot.Field {
	var a updateDepartmentArgs
	bindArgs(args, &a)
	fields := []copilot.Field{describeDepartment(t.deps, cc, a.DepartmentID)}
	if in, err := a.input(); err == nil {
		if in.Name != nil {
			fields = append(fields, copilot.Field{Key: "newName", Value: *in.Name})
		}
		if in.Description != nil {
			fields = append(fields, copilot.Field{Key: "description", Value: *in.Description})
		}
	}
	return fields
}

func (t *updateDepartmentTool) Execute(_ context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a updateDepartmentArgs
	if err := decodeArgs(args, &a); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	in, err := a.input()
	if err != nil {
		return workspaceAdminFailure("update_department", err)
	}
	d, err := t.deps.ScopedDepartments.Update(cc.WorkspaceID, strings.TrimSpace(a.DepartmentID), in)
	if err != nil {
		return workspaceAdminFailure("update_department", err)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{"department_id": d.ID, "name": d.Name}}
}

type deleteDepartmentTool struct{ deps WorkspaceAdminDeps }

func NewDeleteDepartmentTool(deps WorkspaceAdminDeps) copilot.Tool {
	return &deleteDepartmentTool{deps: deps}
}

func (t *deleteDepartmentTool) Meta() copilot.Meta { return departmentsMeta(workspace.ActionDelete, true) }

func (t *deleteDepartmentTool) Definition() tools.Definition {
	return definition("delete_department", "Exclui um departamento. Só depois da aprovação do usuário.", departmentRefArgs{})
}

func (t *deleteDepartmentTool) Validate(_ context.Context, cc copilot.Context, args map[string]interface{}) error {
	a, err := validateArgs[departmentRefArgs](nil, cc, args)
	if err != nil {
		return err
	}
	_, err = t.deps.department(cc, a.DepartmentID)
	return err
}

func (t *deleteDepartmentTool) Describe(_ context.Context, cc copilot.Context, args map[string]interface{}) []copilot.Field {
	var a departmentRefArgs
	bindArgs(args, &a)
	return []copilot.Field{describeDepartment(t.deps, cc, a.DepartmentID)}
}

func (t *deleteDepartmentTool) Execute(_ context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a departmentRefArgs
	if err := decodeArgs(args, &a); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	if err := t.deps.ScopedDepartments.Delete(cc.WorkspaceID, strings.TrimSpace(a.DepartmentID)); err != nil {
		return workspaceAdminFailure("delete_department", err)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{"deleted": true}}
}

type departmentMemberArgs struct {
	departmentRefArgs
	memberRefArgs
}

type departmentMembershipTool struct {
	deps   WorkspaceAdminDeps
	remove bool
}

func NewAddDepartmentMemberTool(deps WorkspaceAdminDeps) copilot.Tool {
	return &departmentMembershipTool{deps: deps}
}

func NewRemoveDepartmentMemberTool(deps WorkspaceAdminDeps) copilot.Tool {
	return &departmentMembershipTool{deps: deps, remove: true}
}

func (t *departmentMembershipTool) Meta() copilot.Meta { return departmentsMeta(workspace.ActionUpdate, true) }

func (t *departmentMembershipTool) Definition() tools.Definition {
	if t.remove {
		return definition("remove_department_member",
			"Tira uma pessoa de um departamento (ela continua no workspace). Só depois da aprovação do usuário.", departmentMemberArgs{})
	}
	return definition("add_department_member", "Coloca uma pessoa do workspace em um departamento. Só depois da aprovação do usuário.", departmentMemberArgs{})
}

func (t *departmentMembershipTool) Validate(_ context.Context, cc copilot.Context, args map[string]interface{}) error {
	a, err := validateArgs[departmentMemberArgs](nil, cc, args)
	if err != nil {
		return err
	}
	if _, err := t.deps.department(cc, a.DepartmentID); err != nil {
		return err
	}
	_, err = t.deps.member(cc, a.UserID)
	return err
}

func (t *departmentMembershipTool) Describe(_ context.Context, cc copilot.Context, args map[string]interface{}) []copilot.Field {
	var a departmentMemberArgs
	bindArgs(args, &a)
	fields := describeMember(t.deps, cc, args)
	return append(fields[:1], describeDepartment(t.deps, cc, a.DepartmentID))
}

func (t *departmentMembershipTool) Execute(_ context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a departmentMemberArgs
	if err := decodeArgs(args, &a); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	target, err := t.deps.member(cc, a.UserID)
	if err != nil {
		return workspaceAdminFailure("department_member", err)
	}
	departmentID := strings.TrimSpace(a.DepartmentID)
	if t.remove {
		err = t.deps.ScopedDepartments.RemoveMember(cc.WorkspaceID, departmentID, target.ID)
	} else {
		_, err = t.deps.ScopedDepartments.AddMember(cc.WorkspaceID, departmentID, wd.AddMemberInput{MemberID: target.ID})
	}
	if err != nil {
		return workspaceAdminFailure("department_member", err)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{"member": memberLabel(target), "done": true}}
}
