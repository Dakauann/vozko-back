package scheduled_message

import (
	"time"

	"vozko/domain/conversation"
)

// WindowReader reports whether a conversation can be replied to right now, and
// if not, why.
//
// Satisfied as-is by the CRM's history provider, which already routes WhatsApp
// to its lead message windows and every other channel to that channel's own
// adapter. Depending on it here rather than re-deriving the rule is what keeps
// one definition of "can we send" behind the composer, the send path and this
// feature — reason included, so a refused schedule explains itself in the same
// words the composer uses.
type WindowReader interface {
	GetWindowStatusForEntry(entryID, entryType string) conversation.WindowState
}

// WakeScheduler asks for a message to be dispatched at a given time.
//
// A best-effort optimisation, not the delivery guarantee: the row is already
// durable when this is called, and the sweep collects anything the queue loses.
// That is why it may fail without failing the schedule.
type WakeScheduler interface {
	ScheduleFire(id string, fireAt time.Time) error
}

// Clock is the source of "now".
//
// Injected rather than called directly so the window arithmetic — the part of
// this feature that is entirely about time — is testable without sleeping.
type Clock interface {
	Now() time.Time
}

// SystemClock is the real clock. Always UTC: every stored instant is UTC, and a
// local-time leak here would shift a delivery by the server's offset.
type SystemClock struct{}

func (SystemClock) Now() time.Time { return time.Now().UTC() }

var _ Clock = SystemClock{}
