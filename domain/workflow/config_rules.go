package workflow

import (
	"encoding/json"
	"fmt"
	"strings"

	"vozko/domain/conversation"
	"vozko/domain/tools"
)

// This file holds the pure, deterministic graph-configuration rules that were
// previously private to the activation usecase (activate_workflow_usecase.go).
// They are relocated here so the activation path AND the AI builder lint share a
// SINGLE implementation per rule — "the builder said it's valid" and "the system
// will accept and run it" are then the same statement by construction. The
// activation usecase keeps thin wrappers that delegate to these functions.

// ValidateNodeScopes ensures every non-trigger, non-decoration node is allowed
// for the workflow type (whatsapp vs voip). A node missing from the catalog is
// treated as incompatible.
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

// ValidateRequiredOutputEdges ensures every non-optional STATIC output handle of
// each node has at least one outgoing edge labeled with that handle id. Dynamic
// handles (text_match cases, ai_agent tools) are not part of a node's static
// catalog Outputs and are validated advisorily by the lint instead.
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

// ValidateRequiredDynamicOutputs ensures every non-optional DYNAMIC output handle
// (handles whose set depends on node config — AI-agent custom-tool routes and the
// agent's "default" response path, text_match cases) is connected. It is the
// dynamic-handle counterpart to ValidateRequiredOutputEdges and, like it, is the
// single backend source of truth: the same rule runs in the builder lint AND at
// activation, so the frontend's notion of "optional" can never let an unhandled
// path reach production. resolveHandles may be nil (no dynamic handles to check).
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

// ValidateSegmentedSendConflict rejects an AI-agent node in segmented
// response_mode that feeds directly into a send_text node — segmented agents
// send messages themselves, so the downstream send_text would duplicate output.
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

// ValidateAIAgentSourceConfig enforces the CONDITIONAL required fields of an
// ai_agent, which depend on its source mode — so they can't be
// static Required fields. It mirrors the executor EXACTLY (ai_agent_executor.go):
// an empty source defaults to "agent"; prompt mode
// (source=="prompt") needs a non-empty model AND instructions; agent mode (any
// other source, including the empty default) needs a non-empty agent_id. PURE, so
// it lives in PureGraphRules and BOTH the builder lint and activation run it,
// keeping them in lockstep. Without it the AI builder would call a model-less
// prompt agent — or, worse, an agent-mode node with no agent_id — "valid", and it
// would only blow up at run time ("agent mode but agent_id is empty"). Activation
// additionally checks the model/agent ids are REAL via repo lookups; this pure
// rule only checks PRESENCE, all the lint can do without a DB.
func ValidateAIAgentSourceConfig(g *Graph) error {
	for i := range g.Nodes {
		n := &g.Nodes[i]
		if n.Type != NodeTypeActionAIAgent {
			continue
		}
		// Mirror the executors: an empty source defaults to "agent" mode.
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
		// Agent mode: agent_id must be present (its VALIDITY is checked at
		// activation by the repo-backed agentValidator).
		if agentID, _ := n.Config["agent_id"].(string); strings.TrimSpace(agentID) == "" {
			// Disambiguate the common mistake the message must teach: the node
			// carries inline prompt config (model/instructions) but source was
			// left in (default) agent mode, so that config is silently ignored
			// and agent_id is what's actually required. Point at the real fix —
			// switch source to "prompt" — instead of just "agent_id missing".
			model, _ := n.Config["model"].(string)
			instr, _ := n.Config["instructions"].(string)
			if strings.TrimSpace(model) != "" || strings.TrimSpace(instr) != "" {
				return fmt.Errorf("%w: node %q is in agent mode (source=%q) but has model/instructions set and no agent_id — these are ignored in agent mode. Set source=\"prompt\" to use that inline prompt, or set agent_id to use a saved agent",
					ErrNodeMissingRequiredField, n.ID, source)
			}
			return fmt.Errorf("%w: node %q field %q", ErrNodeMissingRequiredField, n.ID, "agent_id")
		}
	}
	return nil
}

// ValidateAIAgentToolParams rejects an ai_agent custom_tools
// parameter whose declared type is not one the platform accepts (a semantic alias like
// email/date/enum or a base JSON-Schema type) — otherwise the LLM tool-call
// schema is malformed and the agent fails at run time ("invalid type"). The set
// of valid types is owned by domain/tools (tools.IsValidParamType), the SAME
// source the AI services use to build the tool schema, so the builder and the
// runtime can't disagree. PURE (reads only the node config), so it lives in
// PureGraphRules and the builder lint AND activation both catch it: the AI sees a
// bad param type while building, not only at run time.
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
				if strings.TrimSpace(pType) == "" { // defaults to "string" at run time
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

// ValidateInteractivePromptConfig checks that an interactive prompt node's
// buttons/list options obey WhatsApp's limits (count, unique ids, required
// id/title, per Meta's Cloud API). It reuses conversation.Send*MessageInput.
// Validate — the SAME rules the WhatsApp client enforces at send time — so the AI
// builder sees an invalid options set WHILE BUILDING, not only at run time, and
// there is one source of truth for the numbers. It intentionally does nothing
// when no options are defined yet (an incomplete node is flagged by the
// no-outgoing / required-field rules instead) so a freshly dropped node isn't
// flagged before the author fills it in. PURE, so it runs in BOTH the builder
// lint and activation (registered in PureGraphRules).
func ValidateInteractivePromptConfig(g *Graph) error {
	for i := range g.Nodes {
		n := &g.Nodes[i]
		if !n.Type.IsInteractivePrompt() {
			continue
		}
		// Body presence is the required-field rule's job; substitute a placeholder
		// so this rule surfaces only option problems (and skips length checks on
		// interpolated text, which is resolved at run time).
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

// parseInteractiveButtons decodes the node's `buttons` config (JSON of
// conversation.InteractiveButton, capitalized keys) — the same shape the executor
// and frontend use.
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

// interactiveListRowJSON mirrors the `sections` config JSON authored in the
// frontend (lowercase keys). Kept minimal and local; the numeric limits live in
// domain/conversation and the validation itself is delegated there.
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

// BoolFromConfig coerces a config value to bool, accepting bool, the common
// truthy/falsy strings, and numeric forms. Relocated from the activation usecase.
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
