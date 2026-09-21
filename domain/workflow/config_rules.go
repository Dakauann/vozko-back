package workflow

import (
	"encoding/json"
	"fmt"
	"strings"

	"vozko/domain/conversation"
	"vozko/domain/tools"
)

func ValidateNodeScopes(graph *Graph, wfType WorkflowType, catalog []NodeDefinition) error {
	defs := NodeCatalogMap(catalog)
	for _, node := range graph.Nodes {
		if node.Type.IsTrigger() || node.Type.Category() == NodeCategoryDecoration {
			continue
		}
		def, ok := defs[node.Type]
		if !ok {
			return fmt.Errorf("%w: node %q (%s)", ErrNodeIncompatibleScope, node.ID, node.Type)
		}
		if DefinitionAllowedForType(def, wfType) {
			continue
		}
		return fmt.Errorf("%w: node %q (%s)", ErrNodeIncompatibleScope, node.ID, node.Type)
	}
	return nil
}

func ValidateRequiredOutputEdges(graph *Graph, catalog []NodeDefinition) error {
	defs := NodeCatalogMap(catalog)
	for _, node := range graph.Nodes {
		def, ok := defs[node.Type]
		if !ok || len(def.Outputs) == 0 {
			continue
		}

		edgeLabels := make(map[string]struct{})
		for _, edge := range graph.OutgoingEdges(node.ID) {
			edgeLabels[edge.Label] = struct{}{}
		}

		for _, output := range def.Outputs {
			if output.Optional || strings.TrimSpace(output.ID) == "" {
				continue
			}
			if _, ok := edgeLabels[output.ID]; ok {
				continue
			}
			return fmt.Errorf("%w: node %q output %q", ErrNodeMissingRequiredOutput, node.ID, output.ID)
		}
	}
	return nil
}

func ValidateRequiredDynamicOutputs(graph *Graph, resolveHandles DynamicHandleResolver) error {
	if resolveHandles == nil {
		return nil
	}
	for i := range graph.Nodes {
		node := &graph.Nodes[i]
		handles, ok := resolveHandles(*node)
		if !ok || len(handles) == 0 {
			continue
		}
		connected := make(map[string]struct{})
		for _, edge := range graph.OutgoingEdges(node.ID) {
			connected[edge.Label] = struct{}{}
		}
		for _, h := range handles {
			if h.Optional || strings.TrimSpace(h.ID) == "" {
				continue
			}
			if _, ok := connected[h.ID]; !ok {
				return fmt.Errorf("%w: node %q output %q", ErrNodeMissingRequiredOutput, node.ID, h.ID)
			}
		}
	}
	return nil
}

func ValidateSegmentedSendConflict(g *Graph) error {
	nodeByID := make(map[string]*Node, len(g.Nodes))
	for i := range g.Nodes {
		nodeByID[g.Nodes[i].ID] = &g.Nodes[i]
	}

	for _, n := range g.Nodes {
		if n.Type != NodeTypeActionAIAgent {
			continue
		}
		rm, _ := n.Config["response_mode"].(string)
		if rm != "segmented" {
			continue
		}

		for _, edge := range g.OutgoingEdges(n.ID) {
			if edge.Label != "" && edge.Label != "default" {
				continue
			}
			target := nodeByID[edge.Target]
			if target != nil && target.Type == NodeTypeActionSendText {
				return fmt.Errorf("%w: AI agent node %q → send_text node %q",
					ErrNodeSegmentedSendConflict, n.ID, target.ID)
			}
		}
	}
	return nil
}

func ValidateAIAgentSourceConfig(g *Graph) error {
	for i := range g.Nodes {
		n := &g.Nodes[i]
		if n.Type != NodeTypeActionAIAgent {
			continue
		}
		source, _ := n.Config["source"].(string)
		if source == "" {
			source = "agent"
		}
		if source == "prompt" {
			if model, _ := n.Config["model"].(string); strings.TrimSpace(model) == "" {
				return fmt.Errorf("%w: node %q field %q", ErrNodeMissingRequiredField, n.ID, "model")
			}
			if instr, _ := n.Config["instructions"].(string); strings.TrimSpace(instr) == "" {
				return fmt.Errorf("%w: node %q field %q", ErrNodeMissingRequiredField, n.ID, "instructions")
			}
			continue
		}
		if agentID, _ := n.Config["agent_id"].(string); strings.TrimSpace(agentID) == "" {
			model, _ := n.Config["model"].(string)
			instr, _ := n.Config["instructions"].(string)
			if strings.TrimSpace(model) != "" || strings.TrimSpace(instr) != "" {
				return fmt.Errorf("%w: node %q is in agent mode (source=%q) but has model/instructions set and no agent_id, these are ignored in agent mode. Set source=\"prompt\" to use that inline prompt, or set agent_id to use a saved agent",
					ErrNodeMissingRequiredField, n.ID, source)
			}
			return fmt.Errorf("%w: node %q field %q", ErrNodeMissingRequiredField, n.ID, "agent_id")
		}
	}
	return nil
}

func ValidateAIAgentToolParams(g *Graph) error {
	for i := range g.Nodes {
		n := &g.Nodes[i]
		if n.Type != NodeTypeActionAIAgent {
			continue
		}
		toolList, ok := n.Config["custom_tools"].([]interface{})
		if !ok {
			continue
		}
		for _, rawTool := range toolList {
			tm, ok := rawTool.(map[string]interface{})
			if !ok {
				continue
			}
			toolName, _ := tm["name"].(string)
			params, ok := tm["parameters"].([]interface{})
			if !ok {
				continue
			}
			for _, rawParam := range params {
				pm, ok := rawParam.(map[string]interface{})
				if !ok {
					continue
				}
				pType, _ := pm["type"].(string)
				if strings.TrimSpace(pType) == "" {
					continue
				}
				if !tools.IsValidParamType(pType) {
					pName, _ := pm["name"].(string)
					return fmt.Errorf("%w: node %q tool %q param %q type %q (use um de %v)",
						ErrNodeInvalidToolParamType, n.ID, toolName, pName, strings.TrimSpace(pType), tools.AllowedParamTypes())
				}
			}
		}
	}
	return nil
}

func ValidateInteractivePromptConfig(g *Graph) error {
	for i := range g.Nodes {
		n := &g.Nodes[i]
		if !n.Type.IsInteractivePrompt() {
			continue
		}
		body, _ := n.Config["body"].(string)
		if strings.TrimSpace(body) == "" {
			body = "x"
		}

		if interactivePromptType(n.Config) == "list" {
			sections, perr := parseInteractiveListSections(n.Config)
			if perr != nil {
				return fmt.Errorf("%w: node %q: %v", ErrNodeInvalidInteractiveConfig, n.ID, perr)
			}
			if interactiveListRowCount(sections) == 0 {
				continue
			}
			listButton, _ := n.Config["list_button"].(string)
			input := conversation.SendListMessageInput{
				To:         "lint",
				BodyText:   body,
				ButtonText: strings.TrimSpace(listButton),
				Sections:   sections,
			}
			if verr := input.Validate(); verr != nil {
				return fmt.Errorf("%w: node %q: %v", ErrNodeInvalidInteractiveConfig, n.ID, verr)
			}
			continue
		}

		buttons, perr := parseInteractiveButtons(n.Config)
		if perr != nil {
			return fmt.Errorf("%w: node %q: %v", ErrNodeInvalidInteractiveConfig, n.ID, perr)
		}
		if len(buttons) == 0 {
			continue
		}
		input := conversation.SendButtonMessageInput{
			To:       "lint",
			BodyText: body,
			Buttons:  buttons,
		}
		if verr := input.Validate(); verr != nil {
			return fmt.Errorf("%w: node %q: %v", ErrNodeInvalidInteractiveConfig, n.ID, verr)
		}
	}
	return nil
}

func interactivePromptType(config map[string]interface{}) string {
	t, _ := config["interactive_type"].(string)
	if strings.EqualFold(strings.TrimSpace(t), "list") {
		return "list"
	}
	return "buttons"
}

func parseInteractiveButtons(config map[string]interface{}) ([]conversation.InteractiveButton, error) {
	raw, _ := config["buttons"].(string)
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "[]" {
		return nil, nil
	}
	var buttons []conversation.InteractiveButton
	if err := json.Unmarshal([]byte(raw), &buttons); err != nil {
		return nil, fmt.Errorf("botões inválidos: %v", err)
	}
	return buttons, nil
}

type interactiveListRowJSON struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
}

type interactiveListSectionJSON struct {
	Title string                   `json:"title"`
	Rows  []interactiveListRowJSON `json:"rows"`
}

func parseInteractiveListSections(config map[string]interface{}) ([]conversation.ListSection, error) {
	raw, _ := config["sections"].(string)
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "[]" {
		return nil, nil
	}
	var parsed []interactiveListSectionJSON
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return nil, fmt.Errorf("opções da lista inválidas: %v", err)
	}
	sections := make([]conversation.ListSection, 0, len(parsed))
	for _, s := range parsed {
		rows := make([]conversation.ListRow, 0, len(s.Rows))
		for _, r := range s.Rows {
			rows = append(rows, conversation.ListRow{ID: r.ID, Title: r.Title, Description: r.Description})
		}
		sections = append(sections, conversation.ListSection{Title: s.Title, Rows: rows})
	}
	return sections, nil
}

func interactiveListRowCount(sections []conversation.ListSection) int {
	n := 0
	for _, s := range sections {
		n += len(s.Rows)
	}
	return n
}

func BoolFromConfig(config map[string]interface{}, key string, fallback bool) bool {
	if config == nil {
		return fallback
	}
	v, ok := config[key]
	if !ok || v == nil {
		return fallback
	}
	switch t := v.(type) {
	case bool:
		return t
	case string:
		switch strings.ToLower(strings.TrimSpace(t)) {
		case "true", "1", "yes", "on":
			return true
		case "false", "0", "no", "off", "":
			return false
		}
	case float64:
		return t != 0
	case int:
		return t != 0
	}
	return fallback
}
