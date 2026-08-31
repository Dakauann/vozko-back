package campaign

import (
	"errors"
	"testing"
)

// The full state machine as a table. It is the whole contract of this package,
// so it is asserted exhaustively rather than sampled: every (status, action)
// pair appears exactly once below, and a pair added to ResolveTransition without
// a row here fails TestEveryPairIsCovered.
func TestResolveTransition(t *testing.T) {
	cases := []struct {
		from    Status
		action  Action
		want    Status
		fansOut bool
		wantErr error
	}{
		{StatusRunning, ActionStart, "", false, ErrAlreadyRunning},
		{StatusRunning, ActionPause, StatusPaused, false, nil},
		{StatusRunning, ActionStop, StatusStopped, false, nil},

		{StatusPaused, ActionStart, StatusRunning, true, nil},
		{StatusPaused, ActionPause, "", false, ErrNotRunning},
		{StatusPaused, ActionStop, StatusStopped, false, nil},

		{StatusStopped, ActionStart, StatusRunning, true, nil},
		{StatusStopped, ActionPause, "", false, ErrNotRunning},
		{StatusStopped, ActionStop, "", false, ErrAlreadyStopped},

		// Re-running a finished campaign after a reset is normal. Refusing it
		// would make reset a dead end.
		{StatusCompleted, ActionStart, StatusRunning, true, nil},
		{StatusCompleted, ActionPause, "", false, ErrNotRunning},
		{StatusCompleted, ActionStop, StatusStopped, false, nil},
	}

	for _, c := range cases {
		got, err := ResolveTransition(c.from, c.action)
		if !errors.Is(err, c.wantErr) {
			t.Errorf("%s+%s: err = %v, want %v", c.from, c.action, err, c.wantErr)
			continue
		}
		if c.wantErr != nil {
			continue
		}
		if got.Target != c.want {
			t.Errorf("%s+%s: target = %q, want %q", c.from, c.action, got.Target, c.want)
		}
		if got.Revert != c.from {
			t.Errorf("%s+%s: revert = %q, want %q", c.from, c.action, got.Revert, c.from)
		}
		if got.FansOutWork != c.fansOut {
			t.Errorf("%s+%s: fansOut = %v, want %v", c.from, c.action, got.FansOutWork, c.fansOut)
		}
	}
}

func TestEveryPairIsCovered(t *testing.T) {
	statuses := []Status{StatusRunning, StatusPaused, StatusStopped, StatusCompleted}
	actions := []Action{ActionStart, ActionPause, ActionStop}

	for _, s := range statuses {
		for _, a := range actions {
			tr, err := ResolveTransition(s, a)
			if err == nil && !tr.Target.IsValid() {
				t.Errorf("%s+%s: allowed a transition to an invalid status %q", s, a, tr.Target)
			}
			if err != nil && tr.Target != "" {
				t.Errorf("%s+%s: refused but still returned a target", s, a)
			}
		}
	}
}

// Only START may enqueue work. A PAUSE or STOP that fanned out entries would
// queue the very messages the operator just asked to hold.
func TestOnlyStartFansOutWork(t *testing.T) {
	for _, s := range []Status{StatusRunning, StatusPaused, StatusStopped, StatusCompleted} {
		for _, a := range []Action{ActionPause, ActionStop} {
			tr, err := ResolveTransition(s, a)
			if err == nil && tr.FansOutWork {
				t.Errorf("%s+%s fans out work; only START may", s, a)
			}
		}
	}
}

func TestUnknownActionIsRefused(t *testing.T) {
	if _, err := ResolveTransition(StatusStopped, Action("DELETE")); !errors.Is(err, ErrActionInvalid) {
		t.Fatalf("err = %v, want ErrActionInvalid", err)
	}
}

func TestUnknownStatusIsRefused(t *testing.T) {
	if _, err := ResolveTransition(Status("MIGRATING"), ActionStart); !errors.Is(err, ErrStatusInvalid) {
		t.Fatalf("err = %v, want ErrStatusInvalid", err)
	}
}

// An empty status must not be readable as permission to send.
func TestEmptyStatusNormalizesToStopped(t *testing.T) {
	if got := NormalizeStatus(""); got != StatusStopped {
		t.Fatalf("NormalizeStatus(%q) = %q, want STOPPED", "", got)
	}
	if got := NormalizeStatus("  running "); got != StatusRunning {
		t.Fatalf("NormalizeStatus with whitespace = %q, want RUNNING", got)
	}
}

// A lost compare-and-swap has to report the same refusal the operator would
// have seen had we read the winning status first, or one click reports two
// different things depending on timing.
func TestSwapFailureMatchesTheRefusal(t *testing.T) {
	cases := map[Action]error{
		ActionStart: ErrAlreadyRunning,
		ActionPause: ErrNotRunning,
		ActionStop:  ErrAlreadyStopped,
	}
	for action, want := range cases {
		if got := SwapFailure(action); !errors.Is(got, want) {
			t.Errorf("SwapFailure(%s) = %v, want %v", action, got, want)
		}
	}
}
