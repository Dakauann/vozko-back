package workflow

import (
	"errors"
	"fmt"
	"strings"

	"vozko/domain/choicestats"
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
	OptionKindTemplateButton = choicestats.TemplateButtonOptionPrefix

	InteractiveHandleNoMatch    = choicestats.OptionFreeText
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
	return choicestats.TemplateButtonOptionID(index)
}

func TemplateButtonSelection(text string, index int, matched bool) *OptionSelection {
	id := OptionKindTemplateButton
	if matched {
		id = TemplateButtonOptionID(index)
	}
	return &OptionSelection{ID: id, Title: strings.TrimSpace(text), Kind: OptionKindTemplateButton}
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
	var triggers []string
	for i := range g.Nodes {
		if g.Nodes[i].Type.IsTrigger() {
			triggers = append(triggers, g.Nodes[i].ID)
		}
	}
	return reachedFrom(g, triggers, isReplyEdge)[target]
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
