package conversation

import (
	"testing"
	"time"
)

// The whole point of the reason is that "closed" alone is not actionable, and
// that a time attached to a window means opposite things depending on Open.
// These pin both, because getting either wrong ships copy that lies to an
// operator — which is exactly what happened before the reason existed.

func TestOpenWindowCarriesADeadline(t *testing.T) {
	at := time.Date(2026, 8, 12, 18, 0, 0, 0, time.UTC)

	window := OpenWindow(&at)
	if !window.Open {
		t.Fatal("OpenWindow must be open")
	}
	if window.Reason != WindowReasonNone {
		t.Errorf("reason = %q, want empty on an open window", window.Reason)
	}
	if window.ExpiresAt == nil || !window.ExpiresAt.Equal(at) {
		t.Errorf("expiry = %v, want the deadline", window.ExpiresAt)
	}
}

// A channel with no clock — Telegram in bot mode, a healthy linked device —
// is open with no time at all. Inventing one would make the UI render a
// countdown that means nothing.
func TestOpenWindowWithoutAClock(t *testing.T) {
	window := OpenWindow(nil)
	if !window.Open || window.ExpiresAt != nil {
		t.Fatalf("window = %+v, want open with no deadline", window)
	}
}

func TestClosedWindowAlwaysNamesAReason(t *testing.T) {
	reasons := []WindowClosedReason{
		WindowReasonExpired,
		WindowReasonNoInbound,
		WindowReasonContactBlocked,
		WindowReasonSessionDown,
		WindowReasonReplyRevoked,
		WindowReasonChannelUnavailable,
	}

	for _, reason := range reasons {
		window := ClosedWindow(reason)
		if window.Open {
			t.Errorf("ClosedWindow(%q) reported open", reason)
		}
		if window.Reason != reason {
			t.Errorf("reason = %q, want %q", window.Reason, reason)
		}
		// These closures have no time attached: nothing counts down to them
		// reopening, so a time here would be read as a countdown that never
		// arrives.
		if window.ExpiresAt != nil {
			t.Errorf("%q must not carry a time: only a restriction counts down", reason)
		}
	}
}

// The one closed state that DOES carry a time, and it means the opposite of an
// open window's: blocked UNTIL then, not allowed until then.
func TestClosedWindowUntilCarriesACountdown(t *testing.T) {
	until := time.Date(2026, 8, 12, 20, 0, 0, 0, time.UTC)

	window := ClosedWindowUntil(WindowReasonAccountRestricted, &until)
	if window.Open {
		t.Fatal("a restricted account is closed")
	}
	if window.Reason != WindowReasonAccountRestricted {
		t.Errorf("reason = %q", window.Reason)
	}
	if window.ExpiresAt == nil || !window.ExpiresAt.Equal(until) {
		t.Errorf("expiry = %v, want the moment the restriction lifts", window.ExpiresAt)
	}
}

// Reading a closed window's time as a deadline inverts it exactly. This is the
// distinction every consumer has to respect, so it is stated as a test rather
// than only as a comment.
func TestATimeMeansOppositeThingsOpenVersusClosed(t *testing.T) {
	at := time.Date(2026, 8, 12, 20, 0, 0, 0, time.UTC)

	deadline := OpenWindow(&at)
	countdown := ClosedWindowUntil(WindowReasonAccountRestricted, &at)

	if deadline.ExpiresAt.Equal(*countdown.ExpiresAt) && deadline.Open == countdown.Open {
		t.Fatal("the two states must be distinguishable by Open, not by the time alone")
	}
	if !deadline.Open {
		t.Error("an open window's time is a deadline: sending is allowed UNTIL then")
	}
	if countdown.Open {
		t.Error("a closed window's time is a countdown: sending is forbidden UNTIL then")
	}
}
