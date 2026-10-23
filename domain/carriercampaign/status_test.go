package carriercampaign

import (
	"errors"
	"testing"

	"vozko/domain/campaign"
)

func TestStatusValid(t *testing.T) {
	for _, s := range AllStatuses() {
		if !s.Valid() {
			t.Fatalf("%q should be valid", s)
		}
	}
	for _, s := range []Status{"", "RUNNING", "held", "archived"} {
		if s.Valid() {
			t.Fatalf("%q should be refused", s)
		}
	}
}

func TestStatusPredicates(t *testing.T) {
	cases := []struct {
		status    Status
		editable  bool
		deletable bool
		finished  bool
	}{
		{StatusDraft, true, true, false},
		{StatusReady, true, true, false},
		{StatusScheduled, true, false, false},
		{StatusRunning, false, false, false},
		{StatusPaused, false, false, false},
		{StatusCompleted, false, false, true},
		{StatusStopped, false, false, true},
	}
	for _, tc := range cases {
		if got := tc.status.Editable(); got != tc.editable {
			t.Errorf("%s editable = %v", tc.status, got)
		}
		if got := tc.status.Deletable(); got != tc.deletable {
			t.Errorf("%s deletable = %v", tc.status, got)
		}
		if got := tc.status.Finished(); got != tc.finished {
			t.Errorf("%s finished = %v", tc.status, got)
		}
	}
}

func TestReadyIsAKnownStatus(t *testing.T) {
	if !StatusReady.Valid() || StatusReady != "ready" {
		t.Fatalf("ready = %q", StatusReady)
	}
}

func TestTransition(t *testing.T) {
	cases := []struct {
		name   string
		from   Status
		action Action
		want   Status
		err    error
	}{
		{"draft schedules", StatusDraft, ActionSchedule, StatusScheduled, nil},
		{"scheduled reschedules", StatusScheduled, ActionSchedule, StatusScheduled, nil},
		{"running cannot schedule", StatusRunning, ActionSchedule, "", ErrNotEditable},
		{"scheduled unschedules", StatusScheduled, ActionUnschedule, StatusDraft, nil},
		{"draft cannot unschedule", StatusDraft, ActionUnschedule, "", ErrNotScheduled},
		{"draft starts", StatusDraft, ActionStart, StatusRunning, nil},
		{"scheduled starts", StatusScheduled, ActionStart, StatusRunning, nil},
		{"paused resumes", StatusPaused, ActionStart, StatusRunning, nil},
		{"running cannot start", StatusRunning, ActionStart, "", campaign.ErrAlreadyRunning},
		{"running pauses", StatusRunning, ActionPause, StatusPaused, nil},
		{"paused cannot pause", StatusPaused, ActionPause, "", campaign.ErrNotRunning},
		{"draft cannot pause", StatusDraft, ActionPause, "", campaign.ErrNotRunning},
		{"scheduled cannot pause", StatusScheduled, ActionPause, "", campaign.ErrNotRunning},
		{"running stops", StatusRunning, ActionStop, StatusStopped, nil},
		{"paused stops", StatusPaused, ActionStop, StatusStopped, nil},
		{"scheduled stops", StatusScheduled, ActionStop, StatusStopped, nil},
		{"draft cannot stop", StatusDraft, ActionStop, "", ErrNotLaunched},
		{"running completes", StatusRunning, ActionComplete, StatusCompleted, nil},
		{"paused cannot complete", StatusPaused, ActionComplete, "", campaign.ErrNotRunning},
		{"running waits for the next occurrence", StatusRunning, ActionRecur, StatusScheduled, nil},
		{"paused cannot recur", StatusPaused, ActionRecur, "", campaign.ErrNotRunning},
		{"completed is final", StatusCompleted, ActionStart, "", ErrFinished},
		{"stopped is final", StatusStopped, ActionStop, "", ErrFinished},
		{"unknown status", Status("held"), ActionStart, "", ErrStatusInvalid},
		{"unknown action", StatusDraft, Action("archive"), "", campaign.ErrActionInvalid},
		{"draft becomes ready", StatusDraft, ActionReady, StatusReady, nil},
		{"ready cannot ready again", StatusReady, ActionReady, "", ErrNotDraft},
		{"scheduled cannot ready", StatusScheduled, ActionReady, "", ErrNotDraft},
		{"running cannot ready", StatusRunning, ActionReady, "", ErrNotDraft},
		{"completed cannot ready", StatusCompleted, ActionReady, "", ErrFinished},
		{"ready schedules", StatusReady, ActionSchedule, StatusScheduled, nil},
		{"ready starts", StatusReady, ActionStart, StatusRunning, nil},
		{"ready cannot stop", StatusReady, ActionStop, "", ErrNotLaunched},
		{"ready cannot unschedule", StatusReady, ActionUnschedule, "", ErrNotScheduled},
		{"ready cannot pause", StatusReady, ActionPause, "", campaign.ErrNotRunning},
		{"ready cannot complete", StatusReady, ActionComplete, "", campaign.ErrNotRunning},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Transition(tc.from, tc.action)
			if !errors.Is(err, tc.err) {
				t.Fatalf("err = %v, want %v", err, tc.err)
			}
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}
