package conversation

import (
	"testing"
	"time"
)

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
		if window.ExpiresAt != nil {
			t.Errorf("%q must not carry a time: only a restriction counts down", reason)
		}
	}
}

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
