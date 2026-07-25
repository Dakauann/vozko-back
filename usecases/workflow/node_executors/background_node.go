package node_executors

import "vozko/domain/workflow"

type backgroundNodeExecutor struct{}

func (b backgroundNodeExecutor) Definition() workflow.NodeDefinition {
	return workflow.NodeDefinition{
		Type:        workflow.NodeTypeDecorationBackground,
		Category:    workflow.NodeCategoryDecoration,
		Scopes:      []workflow.NodeScope{workflow.NodeScopeShared},
		Label:       "Plano de Fundo",
		Description: "Elemento de decoração para organizar visualmente o fluxo. Não tem impacto na execução.",
		Icon:        "Square",
		DefaultConfig: map[string]interface{}{
			"color": "#f0f0f0",
		},
		ConfigSchema: []workflow.ConfigField{
			{Key: "color", Label: "Cor de Fundo", Type: "color"},
		},
	}
}

func (b backgroundNodeExecutor) Execute(ctx *workflow.NodeContext) (*workflow.NodeResult, error) {
	return nil, nil
}

func NewBackgroundNodeExecutor() workflow.NodeExecutor {
	return backgroundNodeExecutor{}
}
