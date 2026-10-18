package workflow

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

const (
	ConsentModeKey      = "mode"
	ConsentPurposeKey   = "purpose"
	ConsentNoteKey      = "note"
	ConsentModeGrant    = "registrar"
	ConsentModeWithdraw = "retirar"

	ConsentBranchGranted   = "granted"
	ConsentBranchWithdrawn = "withdrawn"
	ConsentBranchNone      = "none"

	DataKeyTriggerMessageID  = "trigger_message_id"
	OptionKindTemplateButton = "template_button"

	InteractiveHandleNoMatch    = "no_match"
	InteractiveHandleNoReply    = "no_reply"
	InteractiveHandleSendFailed = "send_failed"

	waitHandleReplied = "replied"

	LintConsentGrantNeedsReply LintIssueCode = "NODE_CONSENT_GRANT_NEEDS_REPLY"
)

var ErrConsentGrantNeedsReply = errors.New("workflow: a consent grant must come after the lead's reply on every path (wait for reply, an interactive option or a template button branch)")

func ConsentBranch(optedOut, granted bool) string {
	switch {
	case optedOut:
		return ConsentBranchWithdrawn
	case granted:
		return ConsentBranchGranted
	default:
		return ConsentBranchNone
	}
}

func ApplyTriggerMessage(data map[string]interface{}, messageID string) {
	messageID = strings.TrimSpace(messageID)
	if data == nil || messageID == "" {
		return
	}
	data[DataKeyTriggerMessageID] = messageID
}

func ForgetTriggerMessage(state *RunState) {
	if state != nil {
		state.Delete(DataKeyTriggerMessageID)
	}
}

func TriggerMessageID(state *RunState) string {
	if state == nil {
		return ""
	}
	return strings.TrimSpace(state.GetString(DataKeyTriggerMessageID))
}

func TemplateButtonOptionID(index int) string {
	return OptionKindTemplateButton + ":" + strconv.Itoa(index)
}

func TemplateButtonSelection(text string, index int, matched bool) *OptionSelection {
	id := OptionKindTemplateButton
	if matched {
		id = TemplateButtonOptionID(index)
	}
	return &OptionSelection{ID: id, Title: strings.TrimSpace(text), Kind: OptionKindTemplateButton}
}

func TemplateButtonIndex(optionID string) (int, bool) {
	raw, found := strings.CutPrefix(strings.TrimSpace(optionID), OptionKindTemplateButton+":")
	if !found {
		return 0, false
	}
	index, err := strconv.Atoi(raw)
	if err != nil || index < 0 {
		return 0, false
	}
	return index, true
}

func ValidateConsentModes(g *Graph) error {
	for i := range g.Nodes {
		n := &g.Nodes[i]
		if n.Type != NodeTypeActionConsent {
			continue
		}
		switch consentMode(n) {
		case ConsentModeGrant, ConsentModeWithdraw:
			continue
		}
		return fmt.Errorf("%w: node %q (%s) field %q", ErrNodeMissingRequiredField, n.ID, n.Type, ConsentModeKey)
	}
	return nil
}

func ValidateConsentGrantsFollowAReply(g *Graph) error {
	for i := range g.Nodes {
		n := &g.Nodes[i]
		if n.Type != NodeTypeActionConsent || consentMode(n) != ConsentModeGrant {
			continue
		}
		if reachableWithoutAReply(g, n.ID) {
			return fmt.Errorf("%w: node %q", ErrConsentGrantNeedsReply, n.ID)
		}
	}
	return nil
}

func consentMode(n *Node) string {
	mode, _ := n.Config[ConsentModeKey].(string)
	return strings.TrimSpace(mode)
}

func reachableWithoutAReply(g *Graph, target string) bool {
	var queue []string
	visited := map[string]bool{}
	for i := range g.Nodes {
		if g.Nodes[i].Type.IsTrigger() {
			queue = append(queue, g.Nodes[i].ID)
			visited[g.Nodes[i].ID] = true
		}
	}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		if current == target {
			return true
		}
		source := g.FindNode(current)
		for _, e := range g.OutgoingEdges(current) {
			if visited[e.Target] || isReplyEdge(source, e) {
				continue
			}
			visited[e.Target] = true
			queue = append(queue, e.Target)
		}
	}
	return false
}

func isReplyEdge(source *Node, e Edge) bool {
	if source == nil {
		return false
	}
	switch {
	case source.Type == NodeTypeWaitForReply:
		return e.Label == waitHandleReplied
	case source.Type.IsInteractivePrompt():
		return interactiveReplyHandle(e.Label)
	case source.Type == NodeTypeConditionBranch:
		return branchesOnSelection(source) && (e.Label == "true" || e.Label == "yes")
	}
	return false
}

func interactiveReplyHandle(label string) bool {
	switch label {
	case "", "default", InteractiveHandleNoReply, InteractiveHandleSendFailed:
		return false
	}
	return true
}

func branchesOnSelection(n *Node) bool {
	return readsTheTappedOption(configText(n, "variable")) && matchesExactly(configText(n, "operator")) && namesAnOption(configText(n, "value"))
}

func configText(n *Node, key string) string {
	text, _ := n.Config[key].(string)
	return strings.TrimSpace(text)
}

func readsTheTappedOption(variable string) bool {
	key := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(variable, "{{"), "}}"))
	switch key {
	case DataKeySelectedOptionID, DataKeySelectedOptionTitle, DataKeySelectedOptionKind:
		return true
	}
	return false
}

func matchesExactly(operator string) bool {
	switch operator {
	case "eq", "equals", "==", "eq_ci", "equals_ignore_case":
		return true
	}
	return false
}

func namesAnOption(value string) bool {
	return value != "" && !strings.Contains(value, "{{") && value != OptionKindTemplateButton
}
