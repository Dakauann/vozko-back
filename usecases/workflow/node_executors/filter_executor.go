package node_executors

import (
	"vozko/domain/workflow"
)

type filterExecutor struct{}

func NewFilterExecutor() workflow.NodeExecutor {
	return &filterExecutor{}
}

func (e *filterExecutor) Definition() workflow.NodeDefinition {
	return workflow.NodeDefinition{
		Type:        workflow.NodeTypeConditionFilter,
		Category:    workflow.NodeCategoryCondition,
		Scopes:      []workflow.NodeScope{workflow.NodeScopeShared},
		Label:       "Filtro",
		Description: "Continua o fluxo apenas se a condição for verdadeira. Caso contrário, encerra.",
		Icon:        "Funnel",
		Guidance: workflow.NodeGuidance{
			When:     "Para continuar só se a condição for verdadeira; caso contrário a execução encerra ali.",
			Behavior: "Quando a condição é falsa, a execução do fluxo ENCERRA neste nó; quando verdadeira, segue pela saída para o próximo nó.",
		},
		DefaultConfig: map[string]interface{}{
			"variable": "",
			"operator": "eq",
			"value":    "",
		},
		OutputKeys: []workflow.OutputKeyDefinition{
			{Key: "passed", Description: "true se o filtro deixou passar"},
			{Key: "variable", Description: "Nome da variável avaliada"},
		},
		ConfigSchema: []workflow.ConfigField{
			{Key: "variable", Label: "Variável", Type: "text", Placeholder: "{{node.n2.count}}", Required: true, Description: "Texto ou referência de estado. Use {{...}} para interpolar (ex.: {{node.n2.count}}, {{var.status}})."},
			{Key: "operator", Label: "Operador", Type: "select", Required: true, Options: AllOperators()},
			{Key: "value", Label: "Valor", Type: "text", Placeholder: "ativo"},
		},
	}
}

func (e *filterExecutor) Execute(ctx *workflow.NodeContext) (*workflow.NodeResult, error) {
	variable, _ := ctx.Node.Config["variable"].(string)
	operator, _ := ctx.Node.Config["operator"].(string)
	expected, _ := ctx.Node.Config["value"]

	if variable == "" || operator == "" {
		return nil, workflow.ErrNodeConfigMissing
	}

	if expStr, ok := expected.(string); ok {
		expected = workflow.Interpolate(expStr, ctx.State, nil)
	}

	// Same operand handling as the condition node: {{...}} references interpolate to
	// their value (e.g. {{node.n2.count}}); a bare name still resolves against state
	// (legacy form); a literal that matches no variable is used as-is.
	actual := resolveOperand(variable, ctx.State)
	matched := EvaluateCondition(actual, operator, expected)

	if !matched {

		return &workflow.NodeResult{
			Output:   map[string]interface{}{"passed": false, "variable": variable},
			Complete: true,
		}, nil
	}

	return &workflow.NodeResult{
		Output: map[string]interface{}{"passed": true, "variable": variable},
	}, nil
}
