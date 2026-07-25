package copilottools

import (
	"context"
	"reflect"

	"vozko/domain/agent"
	"vozko/domain/copilot"
	"vozko/domain/tools"
	"vozko/domain/workspace"
)

type createAgentTool struct{ create agent.CreateAgentUseCase }

func NewCreateAgentTool(create agent.CreateAgentUseCase) copilot.Tool {
	return &createAgentTool{create: create}
}

func (t *createAgentTool) Meta() copilot.Meta {
	return copilot.Meta{Mutating: true, Resource: workspace.ResourceAgents, Action: workspace.ActionCreate}
}

func (t *createAgentTool) Definition() tools.Definition {
	params, required := structParams(reflect.TypeOf(agent.CreateAgentInput{}), agentToolDescriptions)
	return tools.Definition{
		Name: "create_agent",
		Description: "Cria um novo agente de IA no workspace. Reúna os campos obrigatórios com o usuário " +
			"antes de chamar; a criação só ocorre após aprovação explícita do usuário.",
		Parameters: params,
		Required:   required,
	}
}

func (t *createAgentTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var in agent.CreateAgentInput
	bindArgs(args, &in)
	in.WorkspaceID = cc.WorkspaceID
	in.InternalTools = []agent.ToolBinding{}
	if in.UseInitialMessage == nil {
		use := in.InitialMessage != ""
		in.UseInitialMessage = &use
	}
	out, err := t.create.Execute(ctx, in)
	if err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	return copilot.Result{Status: copilot.StatusOK, Data: out}
}
