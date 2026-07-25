package node_executors

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"vozko/domain/workflow"
)

type conditionBranchExecutor struct{}

func toFloat64(v interface{}) (float64, bool) {
	switch val := v.(type) {
	case float64:
		return val, true
	case float32:
		return float64(val), true
	case int:
		return float64(val), true
	case int64:
		return float64(val), true
	case string:
		f, err := strconv.ParseFloat(val, 64)
		return f, err == nil
	case bool:
		if val {
			return 1, true
		}
		return 0, true
	default:
		str := fmt.Sprintf("%v", v)
		f, err := strconv.ParseFloat(str, 64)
		return f, err == nil
	}
}

// resolveOperand turns a condition/filter "variable" config field into the value to
// compare. The field is a plain-text template, like every other config field: a
// {{...}} reference is interpolated to its value (e.g. {{node.n2.count}} → "1"). When
// the field contains no {{...}}, it is treated as a bare reference name and resolved
// against state (legacy form, e.g. "interested" or "node.n2.count"). An unresolved
// reference (by either path) yields nil, so empty/null operators behave as before.
// Shared by the condition and filter executors.
func resolveOperand(variable string, state *workflow.RunState) interface{} {
	if strings.Contains(variable, "{{") {
		interp := workflow.Interpolate(variable, state, nil)
		// Interpolate leaves an unresolved reference as its literal "{{...}}" token;
		// treat that as absent (nil) rather than comparing against the raw braces.
		if strings.Contains(interp, "{{") {
			return nil
		}
		return interp
	}
	if resolved, ok := workflow.ResolveVariable(variable, state); ok {
		return resolved
	}
	return nil
}

func EvaluateCondition(actual interface{}, operator string, expected interface{}) bool {
	actualStr := fmt.Sprintf("%v", actual)
	expectedStr := fmt.Sprintf("%v", expected)

	switch operator {

	case "eq", "equals", "==":
		return actualStr == expectedStr
	case "neq", "not_equals", "!=":
		return actualStr != expectedStr
	case "eq_ci", "equals_ignore_case":
		return strings.EqualFold(actualStr, expectedStr)
	case "neq_ci", "not_equals_ignore_case":
		return !strings.EqualFold(actualStr, expectedStr)

	case "contains":
		return strings.Contains(actualStr, expectedStr)
	case "not_contains":
		return !strings.Contains(actualStr, expectedStr)
	case "contains_ci":
		return strings.Contains(strings.ToLower(actualStr), strings.ToLower(expectedStr))
	case "not_contains_ci":
		return !strings.Contains(strings.ToLower(actualStr), strings.ToLower(expectedStr))
	case "starts_with":
		return strings.HasPrefix(actualStr, expectedStr)
	case "ends_with":
		return strings.HasSuffix(actualStr, expectedStr)
	case "starts_with_ci":
		return strings.HasPrefix(strings.ToLower(actualStr), strings.ToLower(expectedStr))
	case "ends_with_ci":
		return strings.HasSuffix(strings.ToLower(actualStr), strings.ToLower(expectedStr))

	case "empty", "is_empty":
		return actualStr == "" || actualStr == "<nil>"
	case "not_empty", "is_not_empty":
		return actualStr != "" && actualStr != "<nil>"
	case "is_null":
		return actual == nil
	case "is_not_null":
		return actual != nil

	case "gt", ">":
		a, aOk := toFloat64(actual)
		b, bOk := toFloat64(expected)
		return aOk && bOk && a > b
	case "gte", ">=":
		a, aOk := toFloat64(actual)
		b, bOk := toFloat64(expected)
		return aOk && bOk && a >= b
	case "lt", "<":
		a, aOk := toFloat64(actual)
		b, bOk := toFloat64(expected)
		return aOk && bOk && a < b
	case "lte", "<=":
		a, aOk := toFloat64(actual)
		b, bOk := toFloat64(expected)
		return aOk && bOk && a <= b

	case "matches", "regex":
		re, err := regexp.Compile(expectedStr)
		if err != nil {
			return false
		}
		return re.MatchString(actualStr)
	case "not_matches", "not_regex":
		re, err := regexp.Compile(expectedStr)
		if err != nil {
			return true
		}
		return !re.MatchString(actualStr)

	case "is_number":
		_, ok := toFloat64(actual)
		return ok
	case "is_not_number":
		_, ok := toFloat64(actual)
		return !ok
	case "is_true":
		return actualStr == "true" || actualStr == "1"
	case "is_false":
		return actualStr == "false" || actualStr == "0" || actualStr == "" || actualStr == "<nil>"

	case "length_eq":
		b, ok := toFloat64(expected)
		return ok && float64(len(actualStr)) == b
	case "length_gt":
		b, ok := toFloat64(expected)
		return ok && float64(len(actualStr)) > b
	case "length_lt":
		b, ok := toFloat64(expected)
		return ok && float64(len(actualStr)) < b
	case "length_gte":
		b, ok := toFloat64(expected)
		return ok && float64(len(actualStr)) >= b
	case "length_lte":
		b, ok := toFloat64(expected)
		return ok && float64(len(actualStr)) <= b

	case "in":
		parts := strings.Split(expectedStr, ",")
		for _, p := range parts {
			if strings.TrimSpace(p) == actualStr {
				return true
			}
		}
		return false
	case "not_in":
		parts := strings.Split(expectedStr, ",")
		for _, p := range parts {
			if strings.TrimSpace(p) == actualStr {
				return false
			}
		}
		return true

	default:
		return false
	}
}

func AllOperators() []workflow.ConfigFieldOption {
	return []workflow.ConfigFieldOption{

		{Value: "eq", Label: "Igual a (==)"},
		{Value: "neq", Label: "Diferente de (!=)"},
		{Value: "eq_ci", Label: "Igual (ignorar maiúsc.)"},
		{Value: "neq_ci", Label: "Diferente (ignorar maiúsc.)"},

		{Value: "contains", Label: "Contém"},
		{Value: "not_contains", Label: "Não contém"},
		{Value: "contains_ci", Label: "Contém (ignorar maiúsc.)"},
		{Value: "starts_with", Label: "Começa com"},
		{Value: "ends_with", Label: "Termina com"},

		{Value: "empty", Label: "Está vazio"},
		{Value: "not_empty", Label: "Não está vazio"},
		{Value: "is_null", Label: "É nulo"},
		{Value: "is_not_null", Label: "Não é nulo"},

		{Value: "gt", Label: "Maior que (>)"},
		{Value: "gte", Label: "Maior ou igual (>=)"},
		{Value: "lt", Label: "Menor que (<)"},
		{Value: "lte", Label: "Menor ou igual (<=)"},

		{Value: "matches", Label: "Corresponde a regex"},
		{Value: "not_matches", Label: "Não corresponde a regex"},

		{Value: "is_number", Label: "É número"},
		{Value: "is_not_number", Label: "Não é número"},
		{Value: "is_true", Label: "É verdadeiro"},
		{Value: "is_false", Label: "É falso"},

		{Value: "length_eq", Label: "Comprimento igual a"},
		{Value: "length_gt", Label: "Comprimento maior que"},
		{Value: "length_lt", Label: "Comprimento menor que"},

		{Value: "in", Label: "Está na lista (vírgula)"},
		{Value: "not_in", Label: "Não está na lista (vírgula)"},
	}
}

func NewConditionBranchExecutor() workflow.NodeExecutor {
	return &conditionBranchExecutor{}
}

func (e *conditionBranchExecutor) Definition() workflow.NodeDefinition {
	return workflow.NodeDefinition{
		Type:        workflow.NodeTypeConditionBranch,
		Category:    workflow.NodeCategoryCondition,
		Scopes:      []workflow.NodeScope{workflow.NodeScopeShared},
		Label:       "Condição",
		Description: "Divide o fluxo com base em uma condição (campo, operador, valor).",
		Icon:        "GitBranch",
		Guidance: workflow.NodeGuidance{
			When: "Para uma decisão booleana sobre uma variável (operadores: eq, contains, gt, empty, matches, in, etc.).",
			Examples: []string{
				"config: {\"variable\":\"{{message}}\",\"operator\":\"contains_ci\",\"value\":\"cancelar\"}  // arestas: rótulo \"true\" e \"false\"",
			},
		},
		Outputs: []workflow.HandleDefinition{
			{ID: "true", Label: "Verdadeiro"},
			{ID: "false", Label: "Falso"},
		},
		DefaultConfig: map[string]interface{}{
			"variable": "",
			"operator": "eq",
			"value":    "",
		},
		OutputKeys: []workflow.OutputKeyDefinition{
			{Key: "matched", Description: "true ou false (resultado da condição)"},
			{Key: "variable", Description: "Nome da variável avaliada"},
			{Key: "operator", Description: "Operador utilizado"},
		},
		ConfigSchema: []workflow.ConfigField{
			{Key: "variable", Label: "Variável", Type: "text", Placeholder: "{{node.n2.count}}", Required: true, Description: "Texto ou referência de estado. Use {{...}} para interpolar (ex.: {{node.n2.count}}, {{var.status}})."},
			{Key: "operator", Label: "Operador", Type: "select", Required: true, Options: AllOperators()},
			{Key: "value", Label: "Valor", Type: "text", Placeholder: "ativo"},
		},
	}
}

func (e *conditionBranchExecutor) Execute(ctx *workflow.NodeContext) (*workflow.NodeResult, error) {
	variable, _ := ctx.Node.Config["variable"].(string)
	operator, _ := ctx.Node.Config["operator"].(string)
	expected, _ := ctx.Node.Config["value"]

	if variable == "" || operator == "" {
		return nil, workflow.ErrNodeConfigMissing
	}

	if expStr, ok := expected.(string); ok {
		expected = workflow.Interpolate(expStr, ctx.State, nil)
	}

	// The "variable" field is a plain-text template like every other config field
	// (and like the "value" field just above): use {{...}} to reference state,
	// e.g. {{node.n2.count}}, and it interpolates to that value before evaluating.
	// When no {{...}} is present we keep the legacy behavior of resolving a bare
	// reference name (e.g. "interested" or "node.n2.count"), so existing conditions
	// don't break; a literal that matches no variable stays as-is.
	actual := resolveOperand(variable, ctx.State)
	matched := EvaluateCondition(actual, operator, expected)

	edges := ctx.Graph.OutgoingEdges(ctx.Node.ID)
	var targetID string
	for _, edge := range edges {
		if matched && (edge.Label == "true" || edge.Label == "yes") {
			targetID = edge.Target
			break
		}
		if !matched && (edge.Label == "false" || edge.Label == "no") {
			targetID = edge.Target
			break
		}
	}
	if targetID == "" && len(edges) > 0 {
		if matched {
			targetID = edges[0].Target
		} else if len(edges) > 1 {
			targetID = edges[1].Target
		} else {
			targetID = edges[0].Target
		}
	}

	return &workflow.NodeResult{
		NextNodeID: targetID,
		Output:     map[string]interface{}{"matched": matched, "variable": variable, "operator": operator},
	}, nil
}
