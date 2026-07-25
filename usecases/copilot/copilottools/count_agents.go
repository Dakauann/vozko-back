package copilottools

import (
	"context"

	"vozko/domain/agent"
	"vozko/domain/copilot"
	"vozko/domain/shared"
	"vozko/domain/tools"
	"vozko/domain/workspace"
)

type countAgentsTool struct{ list agent.ListAgentsUseCase }

func NewCountAgentsTool(list agent.ListAgentsUseCase) copilot.Tool {
	return &countAgentsTool{list: list}
}

func (t *countAgentsTool) Meta() copilot.Meta {
	return copilot.Meta{Mutating: false, Resource: workspace.ResourceAgents, Action: workspace.ActionRead}
}

func (t *countAgentsTool) Definition() tools.Definition {
	return tools.Definition{
		Name:        "count_agents",
		Description: "Conta quantos agentes de IA existem no workspace (mesmos filtros do list_agents). Retorna { total }.",
		Parameters:  agentFilterParams(),
	}
}

func (t *countAgentsTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	in := buildAgentListInput(cc, args)
	in.Options.Pagination = shared.Pagination{Page: 1, PageSize: 1}
	res, err := t.list.Execute(in)
	if err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	var total int64
	if res != nil {
		total = res.TotalItems
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{"total": total}}
}
