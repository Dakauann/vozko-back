package workflow

import (
	"fmt"
	"strings"

	"vozko/domain/channel"
	"vozko/domain/conversation"
	"vozko/domain/tools"
)

func ValidateNodeScopes(graph *Graph, wfType WorkflowType, catalog []NodeDefinition) error {
	return firstProblem(NodeScopeProblems(graph, wfType, catalog))
}

func NodeScopeProblems(graph *Graph, wfType WorkflowType, catalog []NodeDefinition) []error {
	defs := NodeCatalogMap(catalog)
	var problems []error
	for _, node := range graph.Nodes {
		if node.Type.IsTrigger() || node.Type.Category() == NodeCategoryDecoration {
			continue
		}
		if def, ok := defs[node.Type]; ok && DefinitionAllowedForType(def, wfType) {
			continue
		}
		problems = append(problems, fmt.Errorf("%w: node %q (%s)", ErrNodeIncompatibleScope, node.ID, node.Type))
	}
	return problems
}

func ValidateRequiredOutputEdges(graph *Graph, catalog []NodeDefinition) error {
	return firstProblem(RequiredOutputProblems(graph, catalog))
}

func RequiredOutputProblems(graph *Graph, catalog []NodeDefinition) []error {
	defs := NodeCatalogMap(catalog)
	var problems []error
	for i := range graph.Nodes {
		node := &graph.Nodes[i]
		if def, ok := defs[node.Type]; ok {
			problems = append(problems, unconnectedOutputs(graph, node, def.Outputs)...)
		}
	}
	return problems
}

func ValidateRequiredDynamicOutputs(graph *Graph, resolveHandles DynamicHandleResolver) error {
	return firstProblem(RequiredDynamicOutputProblems(graph, resolveHandles))
}

func RequiredDynamicOutputProblems(graph *Graph, resolveHandles DynamicHandleResolver) []error {
	if resolveHandles == nil {
		return nil
	}
	var problems []error
	for i := range graph.Nodes {
		node := &graph.Nodes[i]
		if handles, ok := resolveHandles(*node); ok {
			problems = append(problems, unconnectedOutputs(graph, node, handles)...)
		}
	}
	return problems
}

func unconnectedOutputs(graph *Graph, node *Node, outputs []HandleDefinition) []error {
	if len(outputs) == 0 {
		return nil
	}
	connected := make(map[string]struct{})
	for _, edge := range graph.OutgoingEdges(node.ID) {
		connected[edge.Label] = struct{}{}
	}
	var problems []error
	for _, output := range outputs {
		if output.Optional || strings.TrimSpace(output.ID) == "" {
			continue
		}
		if _, ok := connected[output.ID]; !ok {
			problems = append(problems, &MissingOutputError{NodeID: node.ID, OutputID: output.ID})
		}
	}
	return problems
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

		if InteractivePromptStyle(n.Config) == channel.InteractiveStyleList {
			sections, perr := InteractiveListSections(n.Config)
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

		buttons, perr := InteractiveButtons(n.Config)
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

func interactiveListRowCount(sections []conversation.ListSection) int {
	n := 0
	for _, s := range sections {
		n += len(s.Rows)
	}
	return n
}
