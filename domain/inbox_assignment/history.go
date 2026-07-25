package inbox_assignment

import "time"

// Assignment triggers for history rows (stable codes for metrics).
const (
	TriggerInboundRR    = "inbound_rr"
	TriggerOpen         = "open"
	TriggerManual       = "manual"
	TriggerBulk         = "bulk"
	TriggerCallRoulette = "call_roulette"
	TriggerSystem       = "system"
)

// AssignmentHistory is an ownership interval for one entry.
// Current owner remains in inbox_assignments; this table is append-only intervals.
type AssignmentHistory struct {
	ID                string     `json:"id"`
	WorkspaceID       string     `json:"workspaceId"`
	EntryID           string     `json:"entryId"`
	EntryType         string     `json:"entryType"`
	ActorKind         string     `json:"actorKind"` // human | ai | system
	AssignedActorID   string     `json:"assignedActorId"`
	PreviousActorID   string     `json:"previousActorId,omitempty"`
	Trigger           string     `json:"trigger"`
	AssignedByActorID string     `json:"assignedByActorId,omitempty"`
	BusinessPhoneID   string     `json:"businessPhoneId,omitempty"`
	SIPTrunkID        string     `json:"sipTrunkId,omitempty"`
	DepartmentID      string     `json:"departmentId,omitempty"`
	StartedAt         time.Time  `json:"startedAt"`
	EndedAt           *time.Time `json:"endedAt,omitempty"`
	CreatedAt         time.Time  `json:"createdAt"`
}

// HistoryRepository persists ownership intervals without affecting current assignment UX.
type HistoryRepository interface {
	// CloseOpen ends any open interval for the entry (sets ended_at = now).
	CloseOpen(workspaceID, entryID, entryType string, endedAt time.Time) error
	// Append creates a new open interval (ended_at null).
	Append(h *AssignmentHistory) error
	// ListByEntry returns history newest-first.
	ListByEntry(workspaceID, entryID, entryType string, limit, offset int) ([]*AssignmentHistory, int64, error)
	// GetOpen returns the open interval if any.
	GetOpen(workspaceID, entryID, entryType string) (*AssignmentHistory, error)
}
