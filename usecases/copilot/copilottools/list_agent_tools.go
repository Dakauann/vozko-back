package copilottools

import (
	"context"

	"vozko/domain/copilot"
	"vozko/domain/tools"
	"vozko/domain/workspace"
)

type ToolCatalog interface {
	Definitions() []tools.Definition
	DefinitionsFor(visibility tools.ToolVisibility) []tools.Definition
}

type listAgentToolsTool struct{ catalog ToolCatalog }

func NewListAgentToolsTool(catalog ToolCatalog) copilot.Tool {
	return &listAgentToolsTool{catalog: catalog}
}

func (t *listAgentToolsTool) Meta() copilot.Meta {
	return copilot.Meta{Mutating: false, Resource: workspace.ResourceAIChat, Action: workspace.ActionRead}
}

func (t *listAgentToolsTool) Definition() tools.Definition {
	return tools.Definition{
		Name: "list_agent_tools",
		Description: "Lista as ferramentas internas que um agente pode usar (para preencher internalTools ao criar/editar). " +
			"Retorna todas por padrão; use visibility para filtrar.",
		Parameters: map[string]tools.Parameter{
			"visibility": {
				Type:        "string",
				Description: "filtra por contexto da ferramenta",
				Enum:        []string{string(tools.VisibilityMessaging), string(tools.VisibilityPostConversation)},
			},
		},
	}
}

func (t *listAgentToolsTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var defs []tools.Definition
	switch vis := argString(args, "visibility"); vis {
	case "":
		defs = t.catalog.Definitions()
	case string(tools.VisibilityMessaging), string(tools.VisibilityPostConversation):
		defs = t.catalog.DefinitionsFor(tools.ToolVisibility(vis))
	default:
		return copilot.Result{Status: copilot.StatusError, Message: "visibility inválida (use messaging ou post_conversation)"}
	}
	out := make([]map[string]interface{}, 0, len(defs))
	for _, d := range defs {
		out = append(out, map[string]interface{}{
			"name":           d.Name,
			"displayName":    d.DisplayName,
			"description":    d.Description,
			"requiredConfig": d.RequiredConfig,
			"visibility":     d.Visibility,
		})
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{"items": out, "total": len(out)}}
}
