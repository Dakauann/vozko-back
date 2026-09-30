package node_executors

import (
	"fmt"
	"strings"

	"vozko/domain/actor"
	"vozko/domain/label"
	"vozko/domain/workflow"
)

type assignLabelExecutor struct {
	labeler label.AutomationLabeler
}

func NewAssignLabelExecutor(labeler label.AutomationLabeler) workflow.NodeExecutor {
	return &assignLabelExecutor{labeler: labeler}
}

func (e *assignLabelExecutor) Definition() workflow.NodeDefinition {
	return workflow.NodeDefinition{
		Type:        workflow.NodeTypeActionAssignLabel,
		Category:    workflow.NodeCategoryAction,
		Scopes:      []workflow.NodeScope{workflow.NodeScopeShared},
		Label:       "Gerenciar Etiqueta",
		Description: "Adiciona ou remove uma etiqueta existente da conversa atual do workflow, em qualquer canal.",
		Icon:        "Tag",
		Guidance: workflow.NodeGuidance{
			When:     "Para adicionar ou remover uma etiqueta da conversa.",
			Behavior: "Idempotente: se a conversa já tem a etiqueta (ao adicionar) ou não tem (ao remover), o nó segue pelo caminho de sucesso sem mudar nada.",
		},
		Outputs: []workflow.HandleDefinition{
			{ID: "sucesso", Label: "Sucesso"},
			{ID: "erro", Label: "Erro", Optional: true},
		},
		OutputKeys: []workflow.OutputKeyDefinition{
			{Key: "success", Description: "true quando a conversa fica com a etiqueta pedida"},
			{Key: "changed", Description: "true quando a etiqueta foi de fato adicionada ou removida agora"},
			{Key: "label_id", Description: "ID da etiqueta"},
			{Key: "label_name", Description: "Nome da etiqueta"},
			{Key: "error", Description: "Descrição do erro quando a mudança falha"},
		},
		DefaultConfig: map[string]interface{}{
			"action":   string(label.LabelActionAdd),
			"label_id": "",
		},
		ConfigSchema: []workflow.ConfigField{
			{Key: "action", Label: "Ação", Type: "select", Options: []workflow.ConfigFieldOption{
				{Value: string(label.LabelActionAdd), Label: "Adicionar"},
				{Value: string(label.LabelActionRemove), Label: "Remover"},
			}},
			{Key: "label_id", Label: "Etiqueta", Type: "select", OptionsSource: "labels", Required: true},
		},
	}
}

func (e *assignLabelExecutor) Execute(ctx *workflow.NodeContext) (*workflow.NodeResult, error) {
	labelID, _ := ctx.Node.Config["label_id"].(string)
	if strings.TrimSpace(labelID) == "" {
		return nil, workflow.ErrNodeConfigMissing
	}
	edges := ctx.Graph.OutgoingEdges(ctx.Node.ID)

	rawAction, _ := ctx.Node.Config["action"].(string)
	action, err := label.ParseLabelAction(rawAction)
	if err != nil {
		return labelFailure(edges, err), nil
	}

	change, err := e.labeler.Change(ctx.Run.WorkspaceID, label.LabelChangeRequest{
		Action:    action,
		LabelID:   strings.TrimSpace(workflow.Interpolate(labelID, ctx.State, nil)),
		EntryID:   ctx.Run.EntryID,
		EntryType: ctx.Run.EntryType,
		ActorID:   actor.FormatWorkflow(ctx.Run.WorkflowID),
	})
	if err != nil {
		return labelFailure(edges, err), nil
	}
	return &workflow.NodeResult{
		NextNodeID: resolveEdgeByLabel(edges, "sucesso"),
		Output: map[string]interface{}{
			"success":    true,
			"changed":    !change.Unchanged,
			"label_id":   change.LabelID,
			"label_name": change.LabelName,
		},
	}, nil
}

func labelFailure(edges []workflow.Edge, err error) *workflow.NodeResult {
	return &workflow.NodeResult{
		NextNodeID: resolveEdgeByLabel(edges, "erro"),
		Output: map[string]interface{}{
			"success": false,
			"error":   fmt.Sprintf("erro ao mudar a etiqueta: %v", err),
		},
	}
}
