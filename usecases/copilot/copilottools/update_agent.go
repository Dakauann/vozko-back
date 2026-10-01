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
	card   AgentDeps
}

func NewUpdateAgentTool(get agent.GetAgentUseCase, update agent.UpdateAgentUseCase, card AgentDeps) copilot.Tool {
	return &updateAgentTool{get: get, update: update, card: card}
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
		t.card.secrets().reveal(argToolBindings(args, "addTools"), args),
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

func (t *updateAgentTool) Validate(ctx context.Context, cc copilot.Context, args map[string]interface{}) error {
	if _, err := ownedAgent(t.get, cc, argString(args, "id")); err != nil {
		return err
	}
	if err := t.card.secrets().validate(argToolBindings(args, "addTools")); err != nil {
		return err
	}
	return t.card.requireLinks(ctx, cc.WorkspaceID, argStringList(args, "addKnowledgeBaseIds"), argStringList(args, "addMcpCollectionIds"))
}

func (t *updateAgentTool) Secrets(args map[string]interface{}) []copilot.SecretField {
	return t.card.secrets().fields(argToolBindings(args, "addTools"))
}

func (t *updateAgentTool) Conceal(args map[string]interface{}) map[string]interface{} {
	return t.card.secrets().conceal(args, "addTools")
}

func (t *updateAgentTool) Describe(ctx context.Context, cc copilot.Context, args map[string]interface{}) []copilot.Field {
	var fields agentFields
	bindArgs(args, &fields)
	card := []copilot.Field{describeAgent(t.get, cc, argString(args, "id"))}
	card = append(card, agentFieldCard(fields)...)
	card = append(card, toolBindingField("addTools", argToolBindings(args, "addTools"))...)
	card = append(card, namesField("removeTools", argStringList(args, "removeTools"))...)
	card = append(card, t.card.knowledgeBaseField(ctx, cc, "addKnowledgeBases", argStringList(args, "addKnowledgeBaseIds"))...)
	card = append(card, t.card.knowledgeBaseField(ctx, cc, "removeKnowledgeBases", argStringList(args, "removeKnowledgeBaseIds"))...)
	card = append(card, t.card.collectionField(ctx, cc, "addMcpCollections", argStringList(args, "addMcpCollectionIds"))...)
	return append(card, t.card.collectionField(ctx, cc, "removeMcpCollections", argStringList(args, "removeMcpCollectionIds"))...)
}
