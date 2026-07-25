package node_executors

import (
	"fmt"
	"strings"

	"vozko/domain/label"
	"vozko/domain/workflow"
)

type assignLabelExecutor struct {
	labelRepo label.Repository
}

func NewAssignLabelExecutor(labelRepo label.Repository) workflow.NodeExecutor {
	return &assignLabelExecutor{labelRepo: labelRepo}
}

func (e *assignLabelExecutor) Definition() workflow.NodeDefinition {
	return workflow.NodeDefinition{
		Type:        workflow.NodeTypeActionAssignLabel,
		Category:    workflow.NodeCategoryAction,
		Scopes:      []workflow.NodeScope{workflow.NodeScopeShared},
		Label:       "Atribuir Etiqueta",
		Description: "Atribui uma etiqueta existente à entrada atual do workflow.",
		Icon:        "Tag",
		Guidance: workflow.NodeGuidance{
			When:     "Para atribuir uma etiqueta à conversa (idempotente).",
			Behavior: "Idempotente: se a etiqueta já estiver atribuída, o nó segue normalmente pelo caminho de sucesso.",
		},
		Outputs: []workflow.HandleDefinition{
			{ID: "sucesso", Label: "Sucesso"},
			{ID: "erro", Label: "Erro", Optional: true},
		},
		OutputKeys: []workflow.OutputKeyDefinition{
			{Key: "success", Description: "true quando a etiqueta é atribuída com sucesso"},
			{Key: "label_id", Description: "ID da etiqueta atribuída"},
			{Key: "label_name", Description: "Nome da etiqueta atribuída"},
			{Key: "error", Description: "Descrição do erro quando a atribuição falha"},
		},
		DefaultConfig: map[string]interface{}{
			"label_id": "",
		},
		ConfigSchema: []workflow.ConfigField{
			{Key: "label_id", Label: "Etiqueta", Type: "select", OptionsSource: "labels", Required: true},
		},
	}
}

func (e *assignLabelExecutor) Execute(ctx *workflow.NodeContext) (*workflow.NodeResult, error) {
	labelID, _ := ctx.Node.Config["label_id"].(string)
	if strings.TrimSpace(labelID) == "" {
		return nil, workflow.ErrNodeConfigMissing
	}
	labelID = workflow.Interpolate(labelID, ctx.State, nil)

	edges := ctx.Graph.OutgoingEdges(ctx.Node.ID)

	lbl, err := e.labelRepo.FindByID(labelID)
	if err != nil || lbl == nil {
		errMsg := fmt.Sprintf("etiqueta %q não encontrada", labelID)
		return &workflow.NodeResult{
			NextNodeID: resolveEdgeByLabel(edges, "erro"),
			Output: map[string]interface{}{
				"success": false,
				"error":   errMsg,
			},
		}, nil
	}

	entryLabel := &label.EntryLabel{
		LabelID:     labelID,
		EntryID:     ctx.Run.EntryID,
		EntryType:   ctx.Run.EntryType,
		WorkspaceID: ctx.Run.WorkspaceID,
	}
	if err := e.labelRepo.AssignLabel(entryLabel); err != nil {

		if err == label.ErrEntryLabelExists {
			return &workflow.NodeResult{
				NextNodeID: resolveEdgeByLabel(edges, "sucesso"),
				Output: map[string]interface{}{
					"success":    true,
					"label_id":   lbl.ID,
					"label_name": lbl.Name,
				},
			}, nil
		}
		errMsg := fmt.Sprintf("erro ao atribuir etiqueta: %v", err)
		return &workflow.NodeResult{
			NextNodeID: resolveEdgeByLabel(edges, "erro"),
			Output: map[string]interface{}{
				"success": false,
				"error":   errMsg,
			},
		}, nil
	}

	return &workflow.NodeResult{
		NextNodeID: resolveEdgeByLabel(edges, "sucesso"),
		Output: map[string]interface{}{
			"success":    true,
			"label_id":   lbl.ID,
			"label_name": lbl.Name,
		},
	}, nil
}
