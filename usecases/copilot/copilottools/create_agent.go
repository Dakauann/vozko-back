package copilottools

import (
	"context"

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
	params, required := scalarParams()
	params["internalTools"] = toolListParam(agentFieldDescriptions["internalTools"])
	params["knowledgeBaseIds"] = stringListParam(agentFieldDescriptions["knowledgeBaseIds"])
	params["mcpCollectionIds"] = stringListParam(agentFieldDescriptions["mcpCollectionIds"])

	return tools.Definition{
		Name: "create_agent",
		Description: "Cria um novo agente de IA no workspace. Reúna os campos obrigatórios com o usuário " +
			"antes de chamar; a criação só ocorre após aprovação explícita do usuário.",
		Parameters: params,
		Required:   required,
	}
}

func (t *createAgentTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var fields agentFields
	bindArgs(args, &fields)

	in := fields.toCreateInput()
	in.WorkspaceID = cc.WorkspaceID

	in.InternalTools = []agent.ToolBinding{}
	for _, a := range argToolBindings(args, "internalTools") {
		in.InternalTools = append(in.InternalTools, a.toBinding())
	}
	in.KnowledgeBaseIDs = argStringList(args, "knowledgeBaseIds")
	in.MCPCollectionIDs = argStringList(args, "mcpCollectionIds")

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
