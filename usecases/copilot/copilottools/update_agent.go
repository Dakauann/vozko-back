package copilottools

import (
	"context"

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
	params, _ := scalarParams()
	params["id"] = tools.Parameter{Type: "string", Description: "id do agente a atualizar"}

	params["addTools"] = toolListParam(agentFieldDescriptions["addTools"])
	params["removeTools"] = stringListParam(agentFieldDescriptions["removeTools"])
	params["addKnowledgeBaseIds"] = stringListParam(agentFieldDescriptions["addKnowledgeBaseIds"])
	params["removeKnowledgeBaseIds"] = stringListParam(agentFieldDescriptions["removeKnowledgeBaseIds"])
	params["addMcpCollectionIds"] = stringListParam(agentFieldDescriptions["addMcpCollectionIds"])
	params["removeMcpCollectionIds"] = stringListParam(agentFieldDescriptions["removeMcpCollectionIds"])

	return tools.Definition{
		Name: "update_agent",
		Description: "Atualiza um agente existente. Apenas os campos informados são alterados; os demais permanecem como estão. " +
			"Ferramentas, bases de conhecimento e coleções MCP são gerenciadas de forma incremental (addTools/removeTools etc.): " +
			"as atuais são preservadas automaticamente, não é preciso reenviá-las.",
		Parameters: params,
		Required:   []string{"id"},
	}
}

func (t *updateAgentTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	a, err := ownedAgent(t.get, cc, argString(args, "id"))
	if err != nil {
		return copilot.Result{Status: copilot.StatusDenied, Message: "agente não encontrado neste workspace"}
	}
	id := a.ID

	var fields agentFields
	bindArgs(args, &fields)
	in := fields.toUpdateInput()

	in.InternalTools = mergeToolBindings(
		a.InternalTools,
		argToolBindings(args, "addTools"),
		argStringList(args, "removeTools"),
	)
	in.KnowledgeBaseIDs = mergeStrings(
		a.KnowledgeBaseIDs,
		argStringList(args, "addKnowledgeBaseIds"),
		argStringList(args, "removeKnowledgeBaseIds"),
	)
	in.MCPCollectionIDs = mergeStrings(
		a.MCPCollectionIDs,
		argStringList(args, "addMcpCollectionIds"),
		argStringList(args, "removeMcpCollectionIds"),
	)

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

func (t *updateAgentTool) Validate(_ context.Context, cc copilot.Context, args map[string]interface{}) error {
	_, err := ownedAgent(t.get, cc, argString(args, "id"))
	return err
}
