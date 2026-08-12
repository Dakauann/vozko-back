package scheduled_message

import "time"

// ListQuery filters a workspace-wide listing.
type ListQuery struct {
	Statuses []Status
	Page     int
	PageSize int
}

// Repository persists scheduled messages.
//
// The claim methods are the load-bearing part of the contract. They are not
// "find then update" — an implementation that reads a row and then writes it
// would let two dispatchers both observe `pending` and both send. They must be
// a single conditional write whose affected-row count decides the winner.
type Repository interface {
	Create(m *ScheduledMessage) error
	FindByID(id string) (*ScheduledMessage, error)

	// FindByIdempotencyKey returns the message a previous identical request
	// created, or ErrNotFound. Scoped to the workspace so two tenants cannot
	// collide on a client-chosen key.
	FindByIdempotencyKey(workspaceID, key string) (*ScheduledMessage, error)

	ListByEntry(entryID, entryType string, statuses []Status) ([]*ScheduledMessage, error)
	ListByWorkspace(workspaceID string, q ListQuery) ([]*ScheduledMessage, int64, error)

	// ClaimForDispatch flips exactly one pending row to sending and returns it.
	// Returns (nil, nil) when the row is gone, already claimed, or no longer
	// pending — all of which mean "someone else has this, stop".
	ClaimForDispatch(id string, now time.Time) (*ScheduledMessage, error)

	// ClaimDueBatch does the same for every row past due, bounded by limit.
	ClaimDueBatch(now time.Time, limit int) ([]*ScheduledMessage, error)

	MarkSent(id, messageID string, sentAt time.Time) error
	MarkFailed(id string, reason FailureReason, detail string) error

	// Cancel and Reschedule are guarded on the row still being pending, so a
	// cancel racing a dispatch is resolved by the database rather than by
	// whichever request arrived first.
	Cancel(id string) error
	Reschedule(id string, at time.Time, windowExpiresAt *time.Time) error

	// ListStuckClaims finds rows left in sending by a process that died.
	ListStuckClaims(claimedBefore time.Time, limit int) ([]*ScheduledMessage, error)

	// PurgeTerminalBefore deletes finished rows older than the cutoff.
	PurgeTerminalBefore(cutoff time.Time) (int64, error)
}
