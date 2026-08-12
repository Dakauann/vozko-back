package scheduled_message

import (
	"context"
	"time"
)

// WindowState is what the UI needs to draw the scheduling affordance: whether
// the conversation is open, when it closes, and the latest time it will accept.
//
// LatestAllowedAt is returned rather than left for the client to derive so the
// two cannot disagree about a boundary — a disagreement the operator would
// experience as a date the picker offered and the server refused.
type WindowState struct {
	Open      bool       `json:"open"`
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
	// ClosedReason names why scheduling is refused, in the same vocabulary the
	// composer uses. Empty when the window is open.
	ClosedReason    string     `json:"closedReason,omitempty"`
	LatestAllowedAt *time.Time `json:"latestAllowedAt,omitempty"`
}

// ScheduleInput is a request to park a message.
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

	// IdempotencyKey makes a retried create return the first result instead of
	// a second message. Optional, but the UI always supplies one.
	IdempotencyKey string
}

// ScheduleResult carries the message plus the window it was measured against,
// so a client can render the outcome without a second round trip.
type ScheduleResult struct {
	Message *ScheduledMessage
	Window  WindowState

	// AlreadyExisted is true when an idempotency key matched an earlier request.
	// The caller answers 200 instead of 201; nothing new was created.
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

// ListForEntryResult is the conversation's own scheduled messages plus the live
// window, which is what the composer needs to decide whether to offer
// scheduling at all.
type ListForEntryResult struct {
	Messages []*ScheduledMessage
	Window   WindowState
}

type ListUseCase interface {
	ForEntry(ctx context.Context, entryID, entryType string, statuses []Status) (*ListForEntryResult, error)
	ForWorkspace(ctx context.Context, workspaceID string, q ListQuery) ([]*ScheduledMessage, int64, error)
}

// DispatchUseCase delivers one scheduled message.
//
// Safe to call any number of times for the same id: the first call that wins
// the claim sends, and every other call returns without sending. That is the
// property the queue's at-least-once delivery and the sweep both rely on.
type DispatchUseCase interface {
	Execute(ctx context.Context, id string) error

	// DispatchClaimed delivers a message the caller has ALREADY claimed. The
	// sweep claims in batches — one conditional write for many rows — so
	// re-claiming each row here would both cost a round trip and fail, since
	// the row has already left pending.
	DispatchClaimed(ctx context.Context, m *ScheduledMessage) error
}

// SweepJob is the backstop that runs without the queue: it dispatches anything
// due, retires anything stuck, and expires anything long past.
type SweepJob interface {
	Execute(ctx context.Context) error
}

// PurgeJob drops terminal rows past the retention window.
type PurgeJob interface {
	Execute(ctx context.Context) error
}

// ConsumeFireUseCase subscribes to the delayed-fire queue.
type ConsumeFireUseCase interface {
	Start() error
}
