// Package workflow_infra adapts channel storage to the narrow ports the
// workflow engine declares.
package workflow_infra

import (
	"log"
	"strings"

	wce "vozko/domain/whatsapp_campaign_entry"
	"vozko/domain/workflow"
)

// automationGate answers workflow.AutomationGate for the official WhatsApp
// channel, where a conversation IS a campaign entry and the per-conversation
// automation switch lives on that row.
//
// Only this channel is answered for. Other entry types have no equivalent
// switch on this path, and reporting them as disabled would cancel their runs.
type automationGate struct {
	entries wce.Repository
}

// NewAutomationGate builds the gate the run engine consults when a parked run
// resumes. A nil repository yields a gate that always allows, which is the
// behaviour that predates the check and therefore cannot regress anything.
func NewAutomationGate(entries wce.Repository) workflow.AutomationGate {
	return &automationGate{entries: entries}
}

const entryTypeWhatsApp = "whatsapp"

// AutomationEnabled reports whether the run may continue.
//
// True on every uncertainty, and that direction is deliberate: this is consulted
// on resume, so answering "disabled" cancels the run permanently. A database
// blip must not silence automation for every conversation at once, and a
// channel this gate does not understand must not be governed by it. The switch
// is a pointer precisely so "never toggled" stays distinct from "turned off",
// and only the explicit false stops a run.
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
