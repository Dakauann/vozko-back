package scheduled_message

import "errors"

var (
	ErrNotFound          = errors.New("scheduled message: not found")
	ErrEntryIDRequired   = errors.New("scheduled message: entry id is required")
	ErrEntryTypeInvalid  = errors.New("scheduled message: entry type is invalid")
	ErrWorkspaceRequired = errors.New("scheduled message: workspace id is required")
	ErrSenderRequired    = errors.New("scheduled message: sender user id is required")
	ErrContentRequired   = errors.New("scheduled message: text or media is required")

	ErrWindowClosed = errors.New("scheduled message: the messaging window is closed")

	ErrScheduledAtTooSoon = errors.New("scheduled message: the chosen time is too close to now")

	ErrScheduledAtPastWindow = errors.New("scheduled message: the chosen time falls after the messaging window closes")

	ErrScheduledAtTooFar = errors.New("scheduled message: the chosen time is too far in the future")

	ErrNotPending = errors.New("scheduled message: only a pending message can be changed")
)

var (
	ErrKindInvalid = errors.New("scheduled message: kind must be text or template")

	ErrKindMismatch = errors.New("scheduled message: a text message cannot carry a template")

	ErrTemplateRequired = errors.New("scheduled message: a template message needs a template")

	ErrTemplateWithFreeContent = errors.New("scheduled message: a template cannot carry free text, media, a reply or a signature")

	ErrTemplatesUnsupported = errors.New("scheduled message: this channel does not send templates")

	ErrTemplatePermission = errors.New("scheduled message: sending templates is not allowed for this user")
)
