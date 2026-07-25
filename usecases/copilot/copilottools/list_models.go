package copilottools

import (
	"context"
	"strings"

	"vozko/domain/ai"
	"vozko/domain/copilot"
	"vozko/domain/tools"
	"vozko/domain/workspace"
)

type ModelLister interface {
	GetModelsWithPricing(ctx context.Context) ([]ai.ModelInfo, error)
}

type listModelsTool struct{ models ModelLister }

func NewListModelsTool(models ModelLister) copilot.Tool {
	return &listModelsTool{models: models}
}

func (t *listModelsTool) Meta() copilot.Meta {
	return copilot.Meta{Mutating: false, Resource: workspace.ResourceAIChat, Action: workspace.ActionRead}
}

func (t *listModelsTool) Definition() tools.Definition {
	return tools.Definition{
		Name: "list_models",
		Description: "Lista os modelos de IA disponíveis (id, nome, contexto, preço). Use o id EXATAMENTE como " +
			"retornado ao configurar um agente; nunca invente um id de modelo. Use search para filtrar.",
		Parameters: map[string]tools.Parameter{
			"search": {Type: "string", Description: "filtra os modelos por id ou nome (opcional)"},
		},
	}
}

func (t *listModelsTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	models, err := t.models.GetModelsWithPricing(ctx)
	if err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	q := strings.ToLower(argString(args, "search"))
	if q == "" {
		return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{"items": models, "total": len(models)}}
	}
	out := make([]ai.ModelInfo, 0, len(models))
	for _, m := range models {
		if strings.Contains(strings.ToLower(m.ID+" "+m.Name), q) {
			out = append(out, m)
		}
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{"items": out, "total": len(out)}}
}
