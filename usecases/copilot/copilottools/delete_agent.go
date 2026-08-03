package copilottools

import (
	"context"

	"vozko/domain/agent"
	"vozko/domain/copilot"
	"vozko/domain/tools"
	"vozko/domain/workspace"
)

type deleteAgentTool struct {
	get    agent.GetAgentUseCase
	delete agent.DeleteAgentUseCase
}

func NewDeleteAgentTool(get agent.GetAgentUseCase, del agent.DeleteAgentUseCase) copilot.Tool {
	return &deleteAgentTool{get: get, delete: del}
}

func (t *deleteAgentTool) Meta() copilot.Meta {
	return copilot.Meta{Mutating: true, Resource: workspace.ResourceAgents, Action: workspace.ActionDelete}
}

func (t *deleteAgentTool) Definition() tools.Definition {
	return tools.Definition{
		Name:        "delete_agent",
		Description: "Exclui um agente permanentemente. Ação destrutiva, só ocorre após aprovação explícita do usuário.",
		Parameters:  map[string]tools.Parameter{"id": {Type: "string", Description: "id do agente a excluir"}},
		Required:    []string{"id"},
	}
}

func (t *deleteAgentTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	id := argString(args, "id")
	if id == "" {
		return copilot.Result{Status: copilot.StatusError, Message: "id é obrigatório"}
	}
	a, err := t.get.Execute(id)
	if err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	if a == nil || a.WorkspaceID != cc.WorkspaceID || !inDeptScope(cc.DeptScope, a.DepartmentID) {
		return copilot.Result{Status: copilot.StatusDenied, Message: "agente não encontrado neste workspace"}
	}
	if err := t.delete.Execute(id); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{"id": id, "deleted": true}}
}
