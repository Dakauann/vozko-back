package workflow_infra

import (
	"log"
	"strings"

	wce "vozko/domain/whatsapp_campaign_entry"
	"vozko/domain/workflow"
)

type automationGate struct {
	entries wce.Repository
}

func NewAutomationGate(entries wce.Repository) workflow.AutomationGate {
	return &automationGate{entries: entries}
}

const entryTypeWhatsApp = "whatsapp"

func (g *automationGate) AutomationEnabled(entryID, entryType string) bool {
	if g == nil || g.entries == nil || strings.TrimSpace(entryID) == "" {
		return true
	}
	if t := strings.TrimSpace(strings.ToLower(entryType)); t != "" && t != entryTypeWhatsApp {
		return true
	}

	entry, err := g.entries.FindByID(entryID)
	if err != nil {
		log.Printf("[workflow][automation-gate] could not read entry %s, allowing the run to continue: %v", entryID, err)
		return true
	}
	if entry == nil || entry.AutomationEnabled == nil {
		return true
	}
	return *entry.AutomationEnabled
}
