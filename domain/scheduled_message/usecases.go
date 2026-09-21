package scheduled_message

import (
	"context"
	"time"
)

type WindowState struct {
	Open            bool       `json:"open"`
	ExpiresAt       *time.Time `json:"expiresAt,omitempty"`
	ClosedReason    string     `json:"closedReason,omitempty"`
	LatestAllowedAt *time.Time `json:"latestAllowedAt,omitempty"`
}

type ScheduleInput struct {
	WorkspaceID string
	EntryID     string
	EntryType   string

	CreatedByUserID string

	Text             string
	MediaID          string
	MediaType        string
	ReplyToMessageID string
	Signed           bool

	ScheduledAt time.Time

	IdempotencyKey string
}

type ScheduleResult struct {
	Message *ScheduledMessage
	Window  WindowState

	AlreadyExisted bool
}

type ScheduleUseCase interface {
	Execute(ctx context.Context, in ScheduleInput) (*ScheduleResult, error)
}

type RescheduleInput struct {
	ID          string
	WorkspaceID string
	ScheduledAt time.Time
}

type RescheduleUseCase interface {
	Execute(ctx context.Context, in RescheduleInput) (*ScheduleResult, error)
}

type CancelUseCase interface {
	Execute(ctx context.Context, workspaceID, id string) error
}

type ListForEntryResult struct {
	Messages []*ScheduledMessage
	Window   WindowState
}

type ListUseCase interface {
	ForEntry(ctx context.Context, entryID, entryType string, statuses []Status) (*ListForEntryResult, error)
	ForWorkspace(ctx context.Context, workspaceID string, q ListQuery) ([]*ScheduledMessage, int64, error)
}

type DispatchUseCase interface {
	Execute(ctx context.Context, id string) error

	DispatchClaimed(ctx context.Context, m *ScheduledMessage) error
}

type SweepJob interface {
	Execute(ctx context.Context) error
}

type PurgeJob interface {
	Execute(ctx context.Context) error
}

type ConsumeFireUseCase interface {
	Start() error
}
