package scheduled_message

import "errors"

var (
	ErrNotFound          = errors.New("scheduled message: not found")
	ErrEntryIDRequired   = errors.New("scheduled message: entry id is required")
	ErrEntryTypeInvalid  = errors.New("scheduled message: entry type is invalid")
	ErrWorkspaceRequired = errors.New("scheduled message: workspace id is required")
	ErrSenderRequired    = errors.New("scheduled message: sender user id is required")
	ErrContentRequired   = errors.New("scheduled message: text or media is required")

	// ErrWindowClosed means the conversation cannot be replied to right now.
	// Scheduling is refused rather than accepted-and-hoped: if we could not send
	// this second, we will not promise to send later.
	ErrWindowClosed = errors.New("scheduled message: the messaging window is closed")

	// ErrScheduledAtTooSoon means the chosen time is inside MinScheduleLead.
	// Below that, "schedule" and "send" are the same action.
	ErrScheduledAtTooSoon = errors.New("scheduled message: the chosen time is too close to now")

	// ErrScheduledAtPastWindow means the chosen time falls after the window
	// closes. Callers should surface the latest allowed time alongside it.
	ErrScheduledAtPastWindow = errors.New("scheduled message: the chosen time falls after the messaging window closes")

	// ErrScheduledAtTooFar means the chosen time is beyond MaxScheduleHorizon.
	ErrScheduledAtTooFar = errors.New("scheduled message: the chosen time is too far in the future")

	// ErrNotPending means a terminal or in-flight message was asked to change.
	// Cancelling or rescheduling something already sent is not a partial success.
	ErrNotPending = errors.New("scheduled message: only a pending message can be changed")
)
