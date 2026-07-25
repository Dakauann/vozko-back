package copilottools

import (
	"context"

	"vozko/domain/copilot"
	"vozko/domain/tools"
	"vozko/domain/workspace"
	wd "vozko/domain/workspace/workspace_department"
)

type listDepartmentsTool struct{ list wd.ListDepartmentsUseCase }

func NewListDepartmentsTool(list wd.ListDepartmentsUseCase) copilot.Tool {
	return &listDepartmentsTool{list: list}
}

func (t *listDepartmentsTool) Meta() copilot.Meta {
	return copilot.Meta{Mutating: false, Resource: workspace.ResourceAIChat, Action: workspace.ActionRead}
}

func (t *listDepartmentsTool) Definition() tools.Definition {
	return tools.Definition{
		Name:        "list_departments",
		Description: "Lista os departamentos do workspace que você pode ver (id e nome). Use um id ao criar/editar um agente em um departamento específico.",
	}
}

func (t *listDepartmentsTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	depts, err := t.list.Execute(cc.WorkspaceID)
	if err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	out := make([]wd.Department, 0, len(depts))
	for _, d := range depts {
		if inDeptScope(cc.DeptScope, d.ID) {
			out = append(out, d)
		}
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{"items": out, "total": len(out)}}
}
