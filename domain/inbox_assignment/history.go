package inbox_assignment

import "time"

// Assignment triggers for history rows (stable codes for metrics).
const (
	TriggerInboundRR = "inbound_rr"
	TriggerOpen      = "open"
	TriggerManual    = "manual"
	TriggerBulk      = "bulk"
	TriggerSystem    = "system"
	// TriggerRescue marks a hand-over the rescue sweep made because the
	// previous owner never opened the conversation. Additive on purpose:
	// TriggerInboundRR still marks the original roulette hand-out in both
	// modes, so existing assignment-history metrics keep their meaning.
	TriggerRescue = "rescue"
)

// RescueCandidateTriggers are the ownership origins the rescue sweep may act
// on: a conversation is a candidate while its CURRENT owner received it from
// the roulette, or from a previous rescue hop.
//
// Both, not just the hand-out. A hop closes the inbound_rr interval and opens a
// rescue one, so a set holding only TriggerInboundRR stops the chain dead after
// a single move: MaxRescueHops, the exhaustion path and CountRescuesSinceHandout
// all become unreachable, and the conversation strands on the second agent
// whether or not they ever look at it.
//
// What is left out matters as much. A manual or on-open assignment means a
// human has taken responsibility for the conversation, and quietly taking it
// back off them is the one thing this sweep must never do — so those intervals
// end the chain, which is also how a supervisor stops a rescue by hand.
var RescueCandidateTriggers = []string{TriggerInboundRR, TriggerRescue}

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
	// ListOpenOlderThan returns still-open ownership intervals for the given
	// workspaces whose trigger is one of triggers and which started before
	// olderThan, oldest first and capped at limit.
	//
	// The rescue sweep's candidate query; it passes RescueCandidateTriggers.
	// Oldest first so a saturated batch always makes progress on the
	// conversations that have been waiting longest, rather than re-picking the
	// same arbitrary page every tick.
	ListOpenOlderThan(workspaceIDs []string, triggers []string, olderThan time.Time, limit int) ([]*AssignmentHistory, error)
	// CountRescuesSinceHandout counts how many times the sweep has already
	// moved this entry since the roulette last handed it out.
	//
	// Derived from the interval chain rather than stored on the assignment, so
	// there is no new state to keep consistent: a fresh roulette hand-out
	// naturally resets the count by appearing later than every rescue before it.
	CountRescuesSinceHandout(workspaceID, entryID, entryType string) (int, error)
}
