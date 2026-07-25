package node_executors

import "vozko/domain/workflow"

type setVariableExecutor struct{}

func NewSetVariableExecutor() workflow.NodeExecutor {
	return &setVariableExecutor{}
}

func (e *setVariableExecutor) Definition() workflow.NodeDefinition {
	return workflow.NodeDefinition{
		Type:        workflow.NodeTypeActionSetVariable,
		Category:    workflow.NodeCategoryLogic,
		Scopes:      []workflow.NodeScope{workflow.NodeScopeShared},
		Label:       "Definir Variável",
		Description: "Define uma variável no estado do fluxo.",
		Icon:        "BracketsCurly",
		Guidance: workflow.NodeGuidance{
			When:     "Para gravar/derivar uma variável de fluxo reutilizável adiante.",
			Behavior: "Grava a variável de fluxo (nome em 'variable') com o 'value' já interpolado; fica disponível adiante como {{var.<nome>}}.",
			Examples: []string{
				"config: {\"variable\":\"saudacao\",\"value\":\"Olá {{message}}\"}  // depois use {{var.saudacao}}",
			},
		},
		DefaultConfig: map[string]interface{}{
			"variable": "",
			"value":    "",
		},
		OutputKeys: []workflow.OutputKeyDefinition{
			{Key: "variable", Description: "Nome da variável definida"},
			{Key: "value", Description: "Valor atribuído"},
		},
		ConfigSchema: []workflow.ConfigField{
			{Key: "variable", Label: "Nome da Variável", Type: "text", Placeholder: "minha_variavel", Required: true},
			{Key: "value", Label: "Valor", Type: "text", Placeholder: "valor"},
		},
	}
}

func (e *setVariableExecutor) Execute(ctx *workflow.NodeContext) (*workflow.NodeResult, error) {
	varName, _ := ctx.Node.Config["variable"].(string)
	if varName == "" {
		return nil, workflow.ErrNodeConfigMissing
	}
	value, _ := ctx.Node.Config["value"]
	if strVal, ok := value.(string); ok {
		value = workflow.Interpolate(strVal, ctx.State, nil)
	}
	ctx.State.Set(varName, value)
	return &workflow.NodeResult{
		Output: map[string]interface{}{"variable": varName, "value": value},
	}, nil
}
