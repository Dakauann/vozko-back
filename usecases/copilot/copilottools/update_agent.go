package copilottools

import (
	"context"
	"reflect"

	"vozko/domain/agent"
	"vozko/domain/copilot"
	"vozko/domain/tools"
	"vozko/domain/workspace"
)

type updateAgentTool struct {
	get    agent.GetAgentUseCase
	update agent.UpdateAgentUseCase
}

func NewUpdateAgentTool(get agent.GetAgentUseCase, update agent.UpdateAgentUseCase) copilot.Tool {
	return &updateAgentTool{get: get, update: update}
}

func (t *updateAgentTool) Meta() copilot.Meta {
	return copilot.Meta{Mutating: true, Resource: workspace.ResourceAgents, Action: workspace.ActionUpdate}
}

func (t *updateAgentTool) Definition() tools.Definition {
	params, _ := structParams(reflect.TypeOf(agent.UpdateAgentInput{}), agentToolDescriptions)
	params["id"] = tools.Parameter{Type: "string", Description: "id do agente a atualizar"}
	return tools.Definition{
		Name:        "update_agent",
		Description: "Atualiza um agente existente. Apenas os campos informados são alterados; os demais permanecem como estão.",
		Parameters:  params,
		Required:    []string{"id"},
	}
}

func (t *updateAgentTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
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
	var in agent.UpdateAgentInput
	bindArgs(args, &in)
	if in.InitialMessage != nil && *in.InitialMessage != "" && in.UseInitialMessage == nil {
		enabled := true
		in.UseInitialMessage = &enabled
	}
	out, err := t.update.Execute(ctx, id, in)
	if err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	return copilot.Result{Status: copilot.StatusOK, Data: out}
}
