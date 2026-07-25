package node_executors

import (
	"strings"

	"vozko/domain/label"
	"vozko/domain/workflow"
)

type checkLabelExecutor struct {
	labelRepo label.Repository
}

func NewCheckLabelExecutor(labelRepo label.Repository) workflow.NodeExecutor {
	return &checkLabelExecutor{labelRepo: labelRepo}
}

func (e *checkLabelExecutor) Definition() workflow.NodeDefinition {
	return workflow.NodeDefinition{
		Type:        workflow.NodeTypeConditionCheckLabel,
		Category:    workflow.NodeCategoryCondition,
		Scopes:      []workflow.NodeScope{workflow.NodeScopeShared},
		Label:       "Verificar Etiqueta",
		Description: "Verifica se a entrada possui uma etiqueta específica.",
		Icon:        "TagChevron",
		Guidance: workflow.NodeGuidance{
			When: "Para verificar se a conversa tem uma etiqueta específica.",
		},
		Outputs: []workflow.HandleDefinition{
			{ID: "true", Label: "Possui"},
			{ID: "false", Label: "Não Possui"},
		},
		OutputKeys: []workflow.OutputKeyDefinition{
			{Key: "matched", Description: "true se a entrada possui a etiqueta"},
			{Key: "label_id", Description: "ID da etiqueta verificada"},
			{Key: "label_name", Description: "Nome da etiqueta (se encontrada)"},
		},
		DefaultConfig: map[string]interface{}{
			"label_id": "",
		},
		ConfigSchema: []workflow.ConfigField{
			{Key: "label_id", Label: "Etiqueta", Type: "select", OptionsSource: "labels", Required: true},
		},
	}
}

func (e *checkLabelExecutor) Execute(ctx *workflow.NodeContext) (*workflow.NodeResult, error) {
	labelID, _ := ctx.Node.Config["label_id"].(string)
	if strings.TrimSpace(labelID) == "" {
		return nil, workflow.ErrNodeConfigMissing
	}
	labelID = workflow.Interpolate(labelID, ctx.State, nil)

	edges := ctx.Graph.OutgoingEdges(ctx.Node.ID)

	entryLabels, err := e.labelRepo.GetEntryLabels(ctx.Run.EntryID, ctx.Run.EntryType, ctx.Run.WorkspaceID)
	if err != nil {

		return &workflow.NodeResult{
			NextNodeID: resolveEdgeByLabelStrict(edges, "false"),
			Output: map[string]interface{}{
				"matched":  false,
				"label_id": labelID,
			},
		}, nil
	}

	matched := false
	var matchedName string
	for _, el := range entryLabels {
		if el.LabelID == labelID {
			matched = true
			matchedName = el.LabelName
			break
		}
	}

	targetEdge := "false"
	if matched {
		targetEdge = "true"
	}

	return &workflow.NodeResult{
		NextNodeID: resolveEdgeByLabelStrict(edges, targetEdge),
		Output: map[string]interface{}{
			"matched":    matched,
			"label_id":   labelID,
			"label_name": matchedName,
		},
	}, nil
}
