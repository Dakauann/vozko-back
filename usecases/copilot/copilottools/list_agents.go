package copilottools

import (
	"context"

	"vozko/domain/agent"
	"vozko/domain/copilot"
	"vozko/domain/shared"
	"vozko/domain/tools"
	"vozko/domain/workspace"
)

func agentFilterParams() map[string]tools.Parameter {
	return map[string]tools.Parameter{
		"search":   {Type: "string", Description: "filtra pelo nome do agente"},
		"tag":      {Type: "string", Description: "filtra por uma etiqueta"},
		"isActive": {Type: "boolean", Description: "true só ativos, false só inativos; omita para todos"},
		"archived": {Type: "boolean", Description: "true só arquivados, false só não-arquivados; omita para todos"},
	}
}

func buildAgentListInput(cc copilot.Context, args map[string]interface{}) agent.ListAgentsInput {
	in := agent.ListAgentsInput{
		WorkspaceID:   cc.WorkspaceID,
		DepartmentIDs: cc.DeptScope,
		Search:        argString(args, "search"),
		IsActive:      argBoolPtr(args, "isActive"),
		Archived:      argBoolPtr(args, "archived"),
		Options: shared.QueryOptions{
			Pagination: shared.Pagination{Page: argInt(args, "page"), PageSize: argInt(args, "pageSize")},
		},
	}
	if tag := argString(args, "tag"); tag != "" {
		in.Tags = []string{tag}
	}
	return in
}

type listAgentsTool struct{ list agent.ListAgentsUseCase }

func NewListAgentsTool(list agent.ListAgentsUseCase) copilot.Tool {
	return &listAgentsTool{list: list}
}

func (t *listAgentsTool) Meta() copilot.Meta {
	return copilot.Meta{Mutating: false, Resource: workspace.ResourceAgents, Action: workspace.ActionRead}
}

func (t *listAgentsTool) Definition() tools.Definition {
	p := agentFilterParams()
	p["page"] = tools.Parameter{Type: "integer", Description: "página (padrão 1)"}
	p["pageSize"] = tools.Parameter{Type: "integer", Description: "itens por página (padrão 20, máx. 100)"}
	return tools.Definition{
		Name: "list_agents",
		Description: "Lista os agentes de IA do workspace com filtros e paginação. Retorna os itens da página " +
			"E o total (total_items / total_pages); use o total para responder 'quantos…'.",
		Parameters: p,
	}
}

func (t *listAgentsTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	res, err := t.list.Execute(buildAgentListInput(cc, args))
	if err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	return copilot.Result{Status: copilot.StatusOK, Data: res}
}
