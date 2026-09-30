package tools_usecase

import (
	"context"
	"fmt"
	"log"
	"strings"

	"vozko/domain/label"
	"vozko/domain/tools"
)

const ManageEntryLabelToolName = "manage_entry_label"

type manageEntryLabelTool struct {
	labels  label.ListLabelsUseCase
	labeler label.AutomationLabeler
}

func NewManageEntryLabelTool(labels label.ListLabelsUseCase, labeler label.AutomationLabeler) tools.Handler {
	if labels == nil || labeler == nil {
		return nil
	}
	return &manageEntryLabelTool{labels: labels, labeler: labeler}
}

func (t *manageEntryLabelTool) Definition() tools.Definition {
	return tools.Definition{
		Name:               ManageEntryLabelToolName,
		DisplayName:        "Gerenciar etiquetas da conversa",
		DisplayDescription: "Adiciona ou remove etiquetas existentes do workspace na conversa atual, em qualquer canal.",
		Description: `Adiciona ou remove uma etiqueta existente do workspace na conversa atual.

QUANDO USAR:
- add: a conversa passou a se encaixar no que uma etiqueta descreve (por exemplo VIP, urgente, reclamação).
- remove: uma etiqueta da conversa deixou de valer (por exemplo a reclamação foi resolvida).

Etiquetas não são etapas do funil: uma conversa pode ter várias etiquetas ao mesmo tempo.
Forneça SOMENTE um nome presente no enum do parâmetro "label_name". NUNCA invente etiquetas.`,
		Parameters: map[string]tools.Parameter{
			"action": {
				Type:               "string",
				Description:        "add para colocar a etiqueta, remove para tirar. Sem ação, adiciona.",
				DisplayName:        "Ação",
				DisplayDescription: "Adicionar ou remover a etiqueta",
				Enum:               label.LabelActions(),
			},
			"label_name": {
				Type:               "string",
				Description:        "Nome exato da etiqueta. Use apenas nomes presentes no enum.",
				DisplayName:        "Etiqueta",
				DisplayDescription: "Nome da etiqueta a colocar na conversa",
			},
		},
		Required:   []string{"label_name"},
		Visibility: []tools.ToolVisibility{tools.VisibilityMessaging, tools.VisibilityPostConversation},
		Category:   tools.CategoryAgentUtility,
	}
}

func (t *manageEntryLabelTool) DefinitionWithContext(ctx tools.ToolContext) tools.Definition {
	def := t.Definition()
	if ctx.WorkspaceID == "" {
		return def
	}
	labels, err := t.labels.Execute(ctx.WorkspaceID)
	if err != nil || len(labels) == 0 {
		log.Printf("[ManageEntryLabel] no labels for workspace=%s: %v", ctx.WorkspaceID, err)
		return def
	}
	params := make(map[string]tools.Parameter, len(def.Parameters))
	for key, param := range def.Parameters {
		params[key] = param
	}
	names := params["label_name"]
	names.Enum = labelNames(labels)
	params["label_name"] = names
	def.Parameters = params
	return def
}

func (t *manageEntryLabelTool) Execute(ctx context.Context, params map[string]interface{}) (tools.ExecutionResult, error) {
	return t.ExecuteWithConfig(ctx, nil, params)
}

func (t *manageEntryLabelTool) ExecuteWithConfig(ctx context.Context, config map[string]interface{}, params map[string]interface{}) (tools.ExecutionResult, error) {
	entryID := configString(config, "__entry_id")
	entryType := configString(config, "__entry_type")
	workspaceID := configString(config, "__workspace_id")
	if entryID == "" || entryType == "" || workspaceID == "" {
		return tools.ExecutionResult{Result: "Não foi possível identificar a conversa atual. Esta ferramenta só funciona durante uma conversa.", IsError: true}, nil
	}

	action, err := label.ParseLabelAction(paramString(params, "action"))
	if err != nil {
		return tools.ExecutionResult{Result: "Ação inválida. Use add para adicionar ou remove para remover.", IsError: true}, nil
	}

	labels, err := t.labels.Execute(workspaceID)
	if err != nil {
		return tools.ExecutionResult{Result: "Não foi possível ler as etiquetas do workspace.", IsError: true}, nil
	}
	chosen := findLabel(labels, paramString(params, "label_name"))
	if chosen == nil {
		return tools.ExecutionResult{
			Result:  fmt.Sprintf("Etiqueta não encontrada. Etiquetas disponíveis: %s", strings.Join(labelNames(labels), ", ")),
			IsError: true,
		}, nil
	}

	change, err := t.labeler.Change(workspaceID, label.LabelChangeRequest{
		Action:    action,
		LabelID:   chosen.ID,
		EntryID:   entryID,
		EntryType: entryType,
		ActorID:   automationActor(ctx, config),
	})
	if err != nil {
		log.Printf("[ManageEntryLabel] %s label %s on %s %s failed: %v", action, chosen.ID, entryType, entryID, err)
		return tools.ExecutionResult{Result: fmt.Sprintf("Não foi possível mudar a etiqueta \"%s\".", chosen.Name), IsError: true}, nil
	}
	return tools.ExecutionResult{Result: labelChangeMessage(action, chosen.Name, change.Unchanged)}, nil
}

func labelChangeMessage(action label.LabelAction, name string, unchanged bool) string {
	switch {
	case action == label.LabelActionRemove && unchanged:
		return fmt.Sprintf("A conversa não tinha a etiqueta \"%s\".", name)
	case action == label.LabelActionRemove:
		return fmt.Sprintf("Etiqueta \"%s\" removida da conversa.", name)
	case unchanged:
		return fmt.Sprintf("A conversa já tinha a etiqueta \"%s\".", name)
	}
	return fmt.Sprintf("Etiqueta \"%s\" adicionada à conversa.", name)
}

func findLabel(labels []*label.Label, name string) *label.Label {
	for _, candidate := range labels {
		if candidate != nil && strings.EqualFold(strings.TrimSpace(candidate.Name), name) {
			return candidate
		}
	}
	return nil
}

func labelNames(labels []*label.Label) []string {
	names := make([]string, 0, len(labels))
	for _, l := range labels {
		if l != nil {
			names = append(names, l.Name)
		}
	}
	return names
}

var _ tools.ContextualHandler = (*manageEntryLabelTool)(nil)
