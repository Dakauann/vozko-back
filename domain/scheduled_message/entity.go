package scheduled_message

import (
	"strings"
	"time"

	"vozko/domain/shared"
)

const (
	// MinScheduleLead is the closest a message may be scheduled. Below it,
	// "schedule" and "send" are the same action and the delivery delay is noise;
	// the composer's send button is the right tool.
	MinScheduleLead = time.Minute

	// MaxScheduleHorizon bounds how far ahead a message may be parked on a
	// channel that has no window of its own. It is a sanity limit, not a product
	// rule: on a windowed channel the window always closes first.
	MaxScheduleHorizon = 30 * 24 * time.Hour
)

// Status is where a scheduled message is in its life.
//
//	pending ──claim──▶ sending ──▶ sent
//	   │                   └──────▶ failed
//	   ├──cancel──▶ canceled
//	   └──sweep───▶ failed
//
// Every transition out of pending is a conditional write guarded on the current
// status. That guard, not a lock, is what makes delivery at-most-once: N fire
// signals for one row produce one send, because only one of them can be the
// writer that observes `pending`.
type Status string

const (
	StatusPending  Status = "pending"
	StatusSending  Status = "sending"
	StatusSent     Status = "sent"
	StatusFailed   Status = "failed"
	StatusCanceled Status = "canceled"
)

// IsTerminal reports whether nothing further will happen to this message.
func (s Status) IsTerminal() bool {
	switch s {
	case StatusSent, StatusFailed, StatusCanceled:
		return true
	default:
		return false
	}
}

// CanTransitionTo reports whether a transition is legal.
//
// Deliberately exhaustive rather than "anything but terminal": a message that
// went back from sending to pending would be dispatched twice, which is the one
// outcome this feature must never produce.
func (s Status) CanTransitionTo(next Status) bool {
	switch s {
	case StatusPending:
		return next == StatusSending || next == StatusCanceled || next == StatusFailed
	case StatusSending:
		return next == StatusSent || next == StatusFailed
	default:
		return false
	}
}

// FailureReason names why a scheduled message never reached the customer.
//
// The reasons are distinct because the UI has to say something different for
// each: three of them are "it did not go out", one of them is "we do not know".
type FailureReason string

const (
	// ReasonWindowClosed: the window had closed by the time we tried.
	ReasonWindowClosed FailureReason = "window_closed"
	// ReasonEntryUnavailable: the conversation, contact or account is gone.
	ReasonEntryUnavailable FailureReason = "entry_unavailable"
	// ReasonProviderError: the send call failed.
	ReasonProviderError FailureReason = "provider_error"
	// ReasonDispatchInterrupted: we claimed it, then the process died. The
	// message MAY have been delivered — this is the one reason the UI must not
	// render as a plain failure.
	ReasonDispatchInterrupted FailureReason = "dispatch_interrupted"
)

// ScheduledMessage is one message an operator parked for later.
type ScheduledMessage struct {
	ID          string           `json:"id"`
	WorkspaceID string           `json:"workspaceId"`
	EntryID     string           `json:"entryId"`
	EntryType   shared.EntryType `json:"entryType"`

	// CreatedByUserID is whose message this is. It is also the send attribution:
	// a scheduled message is that operator's message, delivered later, not the
	// system's.
	CreatedByUserID string `json:"createdByUserId"`

	Text             string  `json:"text,omitempty"`
	MediaID          *string `json:"mediaId,omitempty"`
	MediaType        *string `json:"mediaType,omitempty"`
	ReplyToMessageID *string `json:"replyToMessageId,omitempty"`

	// Signed applies the operator's signature at SEND time rather than now, so a
	// deferred message carries their current display name.
	Signed bool `json:"signed"`

	ScheduledAt time.Time `json:"scheduledAt"`

	// WindowExpiresAtAtCreation is the expiry this was validated against, or nil
	// on a channel with no clock. Kept for forensics and for honest UI copy —
	// never re-read as truth, because only the live window is truth.
	WindowExpiresAtAtCreation *time.Time `json:"windowExpiresAtAtCreation,omitempty"`

	Status        Status         `json:"status"`
	FailureReason *FailureReason `json:"failureReason,omitempty"`
	FailureDetail string         `json:"failureDetail,omitempty"`

	// ClaimedAt is the dispatch lease. A row stuck in sending past the lease is
	// one whose dispatcher died.
	ClaimedAt     *time.Time `json:"claimedAt,omitempty"`
	SentAt        *time.Time `json:"sentAt,omitempty"`
	SentMessageID *string    `json:"sentMessageId,omitempty"`

	IdempotencyKey *string `json:"idempotencyKey,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Normalize trims the free-text fields and drops empty optionals, so a caller
// passing "" and a caller passing nil produce the same row.
func (m *ScheduledMessage) Normalize() {
	m.Text = strings.TrimSpace(m.Text)
	m.MediaID = trimmedOrNil(m.MediaID)
	m.MediaType = trimmedOrNil(m.MediaType)
	m.ReplyToMessageID = trimmedOrNil(m.ReplyToMessageID)
	m.IdempotencyKey = trimmedOrNil(m.IdempotencyKey)
	if m.Status == "" {
		m.Status = StatusPending
	}
}

// Validate checks everything that does not depend on the live window.
// ValidateScheduledAt covers the part that does.
func (m *ScheduledMessage) Validate() error {
	if strings.TrimSpace(m.WorkspaceID) == "" {
		return ErrWorkspaceRequired
	}
	if strings.TrimSpace(m.EntryID) == "" {
		return ErrEntryIDRequired
	}
	if !m.EntryType.IsKnown() {
		return ErrEntryTypeInvalid
	}
	if strings.TrimSpace(m.CreatedByUserID) == "" {
		return ErrSenderRequired
	}
	// Same rule the live send path applies, so the two cannot disagree about
	// what an empty message is.
	if m.Text == "" && (m.MediaID == nil || *m.MediaID == "") {
		return ErrContentRequired
	}
	return nil
}

// HasMedia reports whether this message carries an attachment.
func (m *ScheduledMessage) HasMedia() bool {
	return m.MediaID != nil && *m.MediaID != ""
}

// IsDue reports whether the message's time has come.
func (m *ScheduledMessage) IsDue(now time.Time) bool {
	return !m.ScheduledAt.After(now)
}

// LatestAllowed returns the last instant a message may be scheduled for, given
// the conversation's live window state.
//
// Two rules, and the second is the subtle one:
//
//   - A closed window always errors, whatever expiry accompanies it. We could
//     not send this second, so we do not promise to send later.
//   - An expiry bounds scheduling ONLY when the window is open. This is not
//     pedantry: the unofficial WhatsApp adapter reports an expiry while CLOSED
//     (the countdown on a provider restriction), and reading that as a deadline
//     would inverts its meaning entirely. An OPEN window with no expiry is a
//     channel with no clock — Telegram in bot mode, a healthy linked device —
//     and is bounded only by the horizon.
//
// The rule is sound over time because every expiry this system reports is
// anchored on the contact's last inbound message and therefore only ever moves
// forward: a time that fits the window now still fits it at delivery.
func LatestAllowed(open bool, expiresAt *time.Time, now time.Time) (time.Time, error) {
	if !open {
		return time.Time{}, ErrWindowClosed
	}
	horizon := now.Add(MaxScheduleHorizon)
	if expiresAt == nil {
		return horizon, nil
	}
	if expiresAt.Before(horizon) {
		return *expiresAt, nil
	}
	return horizon, nil
}

// ValidateScheduledAt reports whether a chosen time is acceptable right now.
//
// The three failures are distinct errors rather than one, because the operator
// needs a different correction for each: move it later, move it earlier, or
// give up on scheduling entirely.
func ValidateScheduledAt(at time.Time, open bool, expiresAt *time.Time, now time.Time) error {
	latest, err := LatestAllowed(open, expiresAt, now)
	if err != nil {
		return err
	}
	if at.Before(now.Add(MinScheduleLead)) {
		return ErrScheduledAtTooSoon
	}
	if at.After(latest) {
		// A horizon breach and a window breach read the same to the clock but
		// not to the operator: one is "this channel will not hold it that long",
		// the other is "the customer's window closes first".
		if expiresAt != nil && at.After(*expiresAt) {
			return ErrScheduledAtPastWindow
		}
		return ErrScheduledAtTooFar
	}
	return nil
}

func trimmedOrNil(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}
