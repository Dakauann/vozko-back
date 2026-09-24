package conversation

import (
	"strings"

	"vozko/domain/actor"
)

// AutomationProfile is what a conversation's channel says about automation: the
// agent and workflow configured on its campaign or account, and the
// conversation's own pause switch. Every channel projects the same fields.
type AutomationProfile struct {
	AgentID               string
	AgentResponsesEnabled bool
	WorkflowID            string
	WorkflowEnabled       bool
	AutomationEnabled     *bool
}

type AutomationKind string

const (
	AutomationAgent    AutomationKind = "agent"
	AutomationWorkflow AutomationKind = "workflow"
)

// Automation is the agent or workflow that answers a conversation.
type Automation struct {
	Kind AutomationKind
	ID   string
}

// ActorID is the assignee id the automation holds a conversation under: an
// agent is ai:<id>, a workflow is workflow:<id>. The two never share a prefix.
func (g Automation) ActorID() string {
	if g.Kind == AutomationWorkflow {
		return actor.FormatWorkflow(g.ID)
	}
	return actor.FormatAI(g.ID)
}

// Configured names the automation configured for the conversation, paused or
// not. A workflow takes precedence over an agent, as it does on the inbox chip.
func (p AutomationProfile) Configured() (Automation, bool) {
	if id := strings.TrimSpace(p.WorkflowID); p.WorkflowEnabled && id != "" {
		return Automation{Kind: AutomationWorkflow, ID: id}, true
	}
	if id := strings.TrimSpace(p.AgentID); p.AgentResponsesEnabled && id != "" {
		return Automation{Kind: AutomationAgent, ID: id}, true
	}
	return Automation{}, false
}

// Governing is the automation that answers the conversation right now. A nil
// pause switch means the conversation inherits automation, which is on.
func (p AutomationProfile) Governing() (Automation, bool) {
	if p.AutomationEnabled != nil && !*p.AutomationEnabled {
		return Automation{}, false
	}
	return p.Configured()
}

// AutomationProfile is the automation part of an inbox row.
func (e EntryWithLastMessage) AutomationProfile() AutomationProfile {
	return AutomationProfile{
		AgentID:               e.AgentID,
		AgentResponsesEnabled: e.AgentResponsesEnabled,
		WorkflowID:            e.WorkflowID,
		WorkflowEnabled:       e.WorkflowEnabled,
		AutomationEnabled:     e.AutomationEnabled,
	}
}

// EntryAutomationReader reads one conversation's AutomationProfile. It must
// return an error, never a zero profile, when the conversation cannot be read:
// a zero profile means "no automation" and would hand it to a person.
type EntryAutomationReader interface {
	EntryAutomation(entryID, entryType string) (AutomationProfile, error)
}

// ParseResponsibleKind reads the inbox "responsible" filter's automation kind.
// Only an agent (ai) or a workflow is a kind worth filtering by; anything else
// is dropped rather than passed on.
func ParseResponsibleKind(raw string) actor.Kind {
	switch kind := actor.Kind(strings.TrimSpace(raw)); kind {
	case actor.KindAI, actor.KindWorkflow:
		return kind
	}
	return ""
}
