package scheduled_message

import "time"

type ListQuery struct {
	Statuses []Status
	Page     int
	PageSize int
}

type Repository interface {
	Create(m *ScheduledMessage) error
	FindByID(id string) (*ScheduledMessage, error)

	FindByIdempotencyKey(workspaceID, key string) (*ScheduledMessage, error)

	ListByEntry(entryID, entryType string, statuses []Status) ([]*ScheduledMessage, error)
	ListByWorkspace(workspaceID string, q ListQuery) ([]*ScheduledMessage, int64, error)

	ClaimForDispatch(id string, now time.Time) (*ScheduledMessage, error)

	ClaimDueBatch(now time.Time, limit int) ([]*ScheduledMessage, error)

	MarkSent(id, messageID string, sentAt time.Time) error
	MarkFailed(id string, reason FailureReason, detail string) error

	Cancel(id string) error
	Reschedule(id string, at time.Time, windowExpiresAt *time.Time) error

	ListStuckClaims(claimedBefore time.Time, limit int) ([]*ScheduledMessage, error)

	PurgeTerminalBefore(cutoff time.Time) (int64, error)
}
