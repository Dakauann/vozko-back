package copilottools

import (
	"context"

	"vozko/domain/agent"
	"vozko/domain/copilot"
	"vozko/domain/tools"
	"vozko/domain/workspace"
)

type createAgentTool struct {
	create agent.CreateAgentUseCase
	card   AgentDeps
}

func NewCreateAgentTool(create agent.CreateAgentUseCase, card AgentDeps) copilot.Tool {
	return &createAgentTool{create: create, card: card}
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
	for _, a := range t.card.secrets().reveal(argToolBindings(args, "internalTools"), args) {
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

func (t *createAgentTool) Validate(ctx context.Context, cc copilot.Context, args map[string]interface{}) error {
	var fields agentFields
	if err := decodeArgs(args, &fields); err != nil {
		return err
	}
	if err := t.card.secrets().validate(argToolBindings(args, "internalTools")); err != nil {
		return err
	}
	return t.card.requireLinks(ctx, cc.WorkspaceID, argStringList(args, "knowledgeBaseIds"), argStringList(args, "mcpCollectionIds"))
}

func (t *createAgentTool) Secrets(args map[string]interface{}) []copilot.SecretField {
	return t.card.secrets().fields(argToolBindings(args, "internalTools"))
}

func (t *createAgentTool) Conceal(args map[string]interface{}) map[string]interface{} {
	return t.card.secrets().conceal(args, "internalTools")
}

func (t *createAgentTool) Describe(ctx context.Context, cc copilot.Context, args map[string]interface{}) []copilot.Field {
	var fields agentFields
	bindArgs(args, &fields)
	card := agentFieldCard(fields)
	card = append(card, toolBindingField("tools", argToolBindings(args, "internalTools"))...)
	card = append(card, t.card.knowledgeBaseField(ctx, cc, "knowledgeBases", argStringList(args, "knowledgeBaseIds"))...)
	return append(card, t.card.collectionField(ctx, cc, "mcpCollections", argStringList(args, "mcpCollectionIds"))...)
}
