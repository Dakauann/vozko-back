package inbox_assignment

import "time"

const (
	TriggerInboundRR = "inbound_rr"
	TriggerOpen      = "open"
	TriggerManual    = "manual"
	TriggerBulk      = "bulk"
	TriggerSystem    = "system"
	TriggerRescue    = "rescue"

	// TriggerAutomationGoverned: the roulette gave the conversation to the agent
	// or workflow that governs it.
	TriggerAutomationGoverned = "automation_governed"
	// TriggerAutomationHandoff: a workflow handed the conversation to the
	// person it names.
	TriggerAutomationHandoff = "automation_handoff"
	// TriggerAutomationHandoffRoulette: the agent handed the conversation to
	// whoever the roulette picked.
	TriggerAutomationHandoffRoulette = "automation_handoff_rr"
	// TriggerAutomationReleased: the automation was paused and let the conversation go to the team.
	TriggerAutomationReleased = "automation_released"
	// TriggerAutomationResumed: someone switched automation back on and handed
	// the conversation back to the agent or workflow that governs it.
	TriggerAutomationResumed = "automation_resumed"
)

var RescueCandidateTriggers = []string{TriggerInboundRR, TriggerAutomationHandoffRoulette, TriggerRescue}

// IsRouletteHandout reports whether an assignment was dealt by the roulette,
// which starts a rescue chain: the rescue sweep may pass it on if the person
// never opens it, and counts its hops from here.
func IsRouletteHandout(trigger string) bool {
	return trigger == TriggerInboundRR || trigger == TriggerAutomationHandoffRoulette
}

type AssignmentHistory struct {
	ID                string     `json:"id"`
	WorkspaceID       string     `json:"workspaceId"`
	EntryID           string     `json:"entryId"`
	EntryType         string     `json:"entryType"`
	ActorKind         string     `json:"actorKind"`
	AssignedActorID   string     `json:"assignedActorId"`
	PreviousActorID   string     `json:"previousActorId,omitempty"`
	Trigger           string     `json:"trigger"`
	AssignedByActorID string     `json:"assignedByActorId,omitempty"`
	BusinessPhoneID   string     `json:"businessPhoneId,omitempty"`
	DepartmentID      string     `json:"departmentId,omitempty"`
	StartedAt         time.Time  `json:"startedAt"`
	EndedAt           *time.Time `json:"endedAt,omitempty"`
	CreatedAt         time.Time  `json:"createdAt"`
}

type HistoryRepository interface {
	CloseOpen(workspaceID, entryID, entryType string, endedAt time.Time) error
	Append(h *AssignmentHistory) error
	ListByEntry(workspaceID, entryID, entryType string, limit, offset int) ([]*AssignmentHistory, int64, error)
	GetOpen(workspaceID, entryID, entryType string) (*AssignmentHistory, error)
	ListOpenOlderThan(workspaceIDs []string, triggers []string, olderThan time.Time, limit int) ([]*AssignmentHistory, error)
	CountRescuesSinceHandout(workspaceID, entryID, entryType string) (int, error)
}
