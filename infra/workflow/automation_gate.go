package workflow_infra

import (
	"errors"

	"vozko/domain/conversation"
	"vozko/domain/workflow"
)

var errAutomationReaderMissing = errors.New("workflow automation gate: no conversation automation reader wired")

type automationGate struct {
	entries conversation.EntryAutomationReader
}

func NewAutomationGate(entries conversation.EntryAutomationReader) workflow.AutomationGate {
	return &automationGate{entries: entries}
}

func (g *automationGate) AutomationEnabled(entryID, entryType string) (bool, error) {
	if g == nil || g.entries == nil {
		return false, errAutomationReaderMissing
	}
	profile, err := g.entries.EntryAutomation(entryID, entryType)
	if err != nil {
		return false, err
	}
	return conversation.AutomationAllowed(profile.AutomationEnabled), nil
}
