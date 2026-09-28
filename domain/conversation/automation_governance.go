package conversation

import (
	"strings"

	"vozko/domain/actor"
)

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

type Automation struct {
	Kind AutomationKind
	ID   string
}

func (g Automation) ActorID() string {
	if g.Kind == AutomationWorkflow {
		return actor.FormatWorkflow(g.ID)
	}
	return actor.FormatAI(g.ID)
}

func (p AutomationProfile) Configured() (Automation, bool) {
	if id := strings.TrimSpace(p.WorkflowID); p.WorkflowEnabled && id != "" {
		return Automation{Kind: AutomationWorkflow, ID: id}, true
	}
	if id := strings.TrimSpace(p.AgentID); p.AgentResponsesEnabled && id != "" {
		return Automation{Kind: AutomationAgent, ID: id}, true
	}
	return Automation{}, false
}

func (p AutomationProfile) Governing() (Automation, bool) {
	if p.AutomationEnabled != nil && !*p.AutomationEnabled {
		return Automation{}, false
	}
	return p.Configured()
}

func (e EntryWithLastMessage) AutomationProfile() AutomationProfile {
	return AutomationProfile{
		AgentID:               e.AgentID,
		AgentResponsesEnabled: e.AgentResponsesEnabled,
		WorkflowID:            e.WorkflowID,
		WorkflowEnabled:       e.WorkflowEnabled,
		AutomationEnabled:     e.AutomationEnabled,
	}
}

type EntryAutomationReader interface {
	EntryAutomation(entryID, entryType string) (AutomationProfile, error)
}

func ParseResponsibleKind(raw string) actor.Kind {
	switch kind := actor.Kind(strings.TrimSpace(raw)); kind {
	case actor.KindAI, actor.KindWorkflow:
		return kind
	}
	return ""
}
