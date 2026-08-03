package node_executors

import (
	"fmt"
	"log"
	"strings"
	"vozko/domain/workflow"
)

type textMatchExecutor struct{}

func NewTextMatchExecutor() workflow.NodeExecutor {
	return &textMatchExecutor{}
}

func (e *textMatchExecutor) Definition() workflow.NodeDefinition {
	return workflow.NodeDefinition{
		Type:        workflow.NodeTypeConditionTextMatch,
		Category:    workflow.NodeCategoryCondition,
		Scopes:      []workflow.NodeScope{workflow.NodeScopeShared},
		Label:       "Classificar Texto",
		Description: "Roteia o fluxo com base no texto recebido (switch/case).",
		Icon:        "GitBranch",
		Guidance: workflow.NodeGuidance{
			When:     "Para rotear por correspondência de texto (switch/case) sem custo de IA.",
			Behavior: "Saídas DINÂMICAS: cada item de 'cases' cria uma saída (rótulo igual ao value do case), preencha 'cases' ANTES de conectar. Há sempre uma saída 'default' para quando nada corresponde.",
			Examples: []string{
				"config: {\"text\":\"{{message}}\",\"match_mode\":\"exact\",\"cases\":[{\"value\":\"1\"},{\"value\":\"2\"}]}  // arestas: \"1\", \"2\", \"default\"",
			},
		},
		DefaultConfig: map[string]interface{}{
			"text":       "{{message}}",
			"cases":      []interface{}{},
			"match_mode": "exact",
		},
		// Base handle ships in the catalog (the optional "default" fallback). The
		// per-case routes are config-dependent, DynamicHandles tells the frontend
		// to resolve the full set via the backend (TextMatchOutputs).
		Outputs: []workflow.HandleDefinition{
			{ID: "default", Label: "Padrão", Optional: true},
		},
		DynamicHandles: true,
		OutputKeys: []workflow.OutputKeyDefinition{
			{Key: "matched", Description: "Valor do caso que correspondeu (vazio se padrão)"},
			{Key: "text", Description: "Texto avaliado"},
		},
		ConfigSchema: []workflow.ConfigField{
			{Key: "text", Label: "Texto", Type: "text", Placeholder: "{{message}}", Required: true},
			{Key: "cases", Label: "Casos", Type: "cases", Placeholder: ""},
			{Key: "match_mode", Label: "Modo de comparação", Type: "select", Placeholder: "exact", Options: []workflow.ConfigFieldOption{
				{Value: "exact", Label: "Exato"},
				{Value: "contains", Label: "Contém"},
			}},
		},
	}
}

func (e *textMatchExecutor) Execute(ctx *workflow.NodeContext) (*workflow.NodeResult, error) {
	textTpl, _ := ctx.Node.Config["text"].(string)
	if textTpl == "" {
		textTpl = "{{message}}"
	}

	text := strings.TrimSpace(workflow.Interpolate(textTpl, ctx.State, nil))
	textLower := strings.ToLower(text)

	matchMode, _ := ctx.Node.Config["match_mode"].(string)
	if matchMode == "" {
		matchMode = "exact"
	}

	log.Printf("[text_match] node %s: matching text=%q mode=%s", ctx.Node.ID, text, matchMode)

	casesRaw, _ := ctx.Node.Config["cases"].([]interface{})

	edges := ctx.Graph.OutgoingEdges(ctx.Node.ID)

	for _, c := range casesRaw {
		caseMap, ok := c.(map[string]interface{})
		if !ok {
			continue
		}
		value, _ := caseMap["value"].(string)
		if value == "" {
			continue
		}

		valueLower := strings.ToLower(strings.TrimSpace(value))
		var matched bool
		switch matchMode {
		case "contains":
			matched = strings.Contains(textLower, valueLower)
		default:
			matched = textLower == valueLower
		}

		if matched {
			label := value
			nextID := resolveEdgeByLabel(edges, label)
			log.Printf("[text_match] node %s: matched case=%q → next=%s", ctx.Node.ID, value, nextID)
			return &workflow.NodeResult{
				NextNodeID: nextID,
				Output:     map[string]interface{}{"matched": value, "text": text},
			}, nil
		}
	}

	nextID := resolveEdgeByLabel(edges, "default")
	log.Printf("[text_match] node %s: no match, using default → next=%s", ctx.Node.ID, nextID)
	return &workflow.NodeResult{
		NextNodeID: nextID,
		Output:     map[string]interface{}{"matched": "", "text": text, "fallback": true},
	}, nil
}

func TextMatchOutputs(config map[string]interface{}) []workflow.HandleDefinition {
	casesRaw, _ := config["cases"].([]interface{})
	outputs := make([]workflow.HandleDefinition, 0, len(casesRaw)+1)

	for _, c := range casesRaw {
		caseMap, ok := c.(map[string]interface{})
		if !ok {
			continue
		}
		value, _ := caseMap["value"].(string)
		if value == "" {
			continue
		}
		outputs = append(outputs, workflow.HandleDefinition{
			ID:    value,
			Label: fmt.Sprintf("%s", value),
		})
	}

	// "default" is the optional fallback (unmatched input): a flow may legitimately
	// handle only specific cases and let the rest end. Declared cases above, by
	// contrast, are required, a case that routes nowhere is a dead-end.
	outputs = append(outputs, workflow.HandleDefinition{
		ID:       "default",
		Label:    "Padrão",
		Optional: true,
	})

	return outputs
}
