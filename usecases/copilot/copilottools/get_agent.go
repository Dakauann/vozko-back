package copilottools

import (
	"context"

	"vozko/domain/agent"
	"vozko/domain/copilot"
	"vozko/domain/tools"
	"vozko/domain/workspace"
)

type getAgentTool struct{ get agent.GetAgentUseCase }

func NewGetAgentTool(get agent.GetAgentUseCase) copilot.Tool {
	return &getAgentTool{get: get}
}

func (t *getAgentTool) Meta() copilot.Meta {
	return copilot.Meta{Mutating: false, Resource: workspace.ResourceAgents, Action: workspace.ActionRead}
}

func (t *getAgentTool) Definition() tools.Definition {
	return tools.Definition{
		Name:        "get_agent",
		Description: "Retorna os detalhes completos de um agente de IA pelo seu id.",
		Parameters:  map[string]tools.Parameter{"id": {Type: "string", Description: "id do agente"}},
		Required:    []string{"id"},
	}
}

func (t *getAgentTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
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
	return copilot.Result{Status: copilot.StatusOK, Data: a}
}
