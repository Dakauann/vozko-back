package inbox_assignment

import "time"

const (
	TriggerInboundRR = "inbound_rr"
	TriggerOpen      = "open"
	TriggerManual    = "manual"
	TriggerBulk      = "bulk"
	TriggerSystem    = "system"
	TriggerRescue    = "rescue"
	TriggerOutreach  = "inbound_outreach"

	TriggerOutreachSent = "outreach_sent"

	TriggerAutomationGoverned        = "automation_governed"
	TriggerAutomationHandoff         = "automation_handoff"
	TriggerAutomationHandoffRoulette = "automation_handoff_rr"
	TriggerAutomationReleased        = "automation_released"
	TriggerAutomationTakenOver       = "automation_taken_over"
	TriggerAutomationResumed         = "automation_resumed"
)

var RescueCandidateTriggers = []string{TriggerInboundRR, TriggerAutomationHandoffRoulette, TriggerRescue}

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
