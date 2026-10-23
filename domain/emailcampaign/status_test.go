package emailcampaign

import (
	"errors"
	"testing"

	"vozko/domain/campaign"
)

func TestTransitionTable(t *testing.T) {
	cases := []struct {
		from    Status
		action  Action
		want    Status
		wantErr error
	}{
		{StatusDraft, ActionSchedule, StatusScheduled, nil},
		{StatusDraft, ActionStart, StatusRunning, nil},
		{StatusDraft, ActionPause, "", campaign.ErrNotRunning},
		{StatusDraft, ActionStop, "", ErrNotLaunched},
		{StatusDraft, ActionHold, "", campaign.ErrNotRunning},
		{StatusDraft, ActionComplete, "", campaign.ErrNotRunning},
		{StatusDraft, ActionUnschedule, "", ErrNotScheduled},
		{StatusDraft, ActionReady, StatusReady, nil},

		{StatusReady, ActionStart, StatusRunning, nil},
		{StatusReady, ActionSchedule, StatusScheduled, nil},
		{StatusReady, ActionReady, "", ErrNotDraft},
		{StatusReady, ActionStop, "", ErrNotLaunched},
		{StatusReady, ActionUnschedule, "", ErrNotScheduled},
		{StatusReady, ActionPause, "", campaign.ErrNotRunning},
		{StatusScheduled, ActionReady, "", ErrNotDraft},
		{StatusRunning, ActionReady, "", ErrNotDraft},
		{StatusStopped, ActionReady, "", ErrFinished},

		{StatusScheduled, ActionStart, StatusRunning, nil},
		{StatusScheduled, ActionUnschedule, StatusDraft, nil},
		{StatusScheduled, ActionStop, StatusStopped, nil},
		{StatusScheduled, ActionSchedule, StatusScheduled, nil},
		{StatusScheduled, ActionPause, "", campaign.ErrNotRunning},

		{StatusRunning, ActionStart, "", campaign.ErrAlreadyRunning},
		{StatusRunning, ActionPause, StatusPaused, nil},
		{StatusRunning, ActionHold, StatusHeld, nil},
		{StatusRunning, ActionStop, StatusStopped, nil},
		{StatusRunning, ActionComplete, StatusCompleted, nil},
		{StatusRunning, ActionSchedule, "", ErrNotDraft},

		{StatusHeld, ActionStart, StatusRunning, nil},
		{StatusHeld, ActionPause, StatusPaused, nil},
		{StatusHeld, ActionHold, "", ErrAlreadyHeld},
		{StatusHeld, ActionStop, StatusStopped, nil},
		{StatusHeld, ActionComplete, "", campaign.ErrNotRunning},

		{StatusPaused, ActionStart, StatusRunning, nil},
		{StatusPaused, ActionPause, "", campaign.ErrNotRunning},
		{StatusPaused, ActionHold, "", campaign.ErrNotRunning},
		{StatusPaused, ActionStop, StatusStopped, nil},

		{StatusCompleted, ActionStart, "", ErrFinished},
		{StatusCompleted, ActionStop, "", ErrFinished},
		{StatusCompleted, ActionPause, "", ErrFinished},
		{StatusStopped, ActionStart, "", ErrFinished},
		{StatusStopped, ActionStop, "", ErrFinished},
		{StatusStopped, ActionSchedule, "", ErrFinished},

		{"bogus", ActionStart, "", ErrStatusInvalid},
		{"validating", ActionStart, "", ErrStatusInvalid},
		{StatusDraft, "jump", "", campaign.ErrActionInvalid},
		{StatusDraft, "validate", "", campaign.ErrActionInvalid},
	}
	for _, tc := range cases {
		t.Run(string(tc.from)+"/"+string(tc.action), func(t *testing.T) {
			got, err := Transition(tc.from, tc.action)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

func TestStatusGroups(t *testing.T) {
	for _, s := range AllStatuses() {
		if !s.Valid() {
			t.Errorf("%q must be valid", s)
		}
	}
	editable := map[Status]bool{StatusDraft: true, StatusScheduled: true, StatusReady: true}
	deletable := map[Status]bool{StatusDraft: true, StatusReady: true}
	launched := map[Status]bool{StatusRunning: true, StatusPaused: true, StatusHeld: true, StatusCompleted: true, StatusStopped: true}
	for _, s := range AllStatuses() {
		if s.Editable() != editable[s] {
			t.Errorf("%q editable = %v", s, s.Editable())
		}
		if s.Launched() != launched[s] {
			t.Errorf("%q launched = %v", s, s.Launched())
		}
		if s.Deletable() != deletable[s] {
			t.Errorf("%q deletable = %v", s, s.Deletable())
		}
	}
	if !StatusRunning.Sending() || !StatusHeld.Sending() || StatusPaused.Sending() {
		t.Fatal("running and held campaigns are sending; paused ones are not")
	}
}

func TestLiveStatusesKeepTheSenderAwake(t *testing.T) {
	want := map[Status]bool{StatusScheduled: true, StatusRunning: true, StatusHeld: true}
	for _, s := range AllStatuses() {
		if got := s.KeepsSenderAwake(); got != want[s] {
			t.Errorf("%s.KeepsSenderAwake() = %v, want %v", s, got, want[s])
		}
	}
	for _, s := range LiveStatuses() {
		if !want[s] {
			t.Errorf("LiveStatuses has %s", s)
		}
	}
	if len(LiveStatuses()) != len(want) {
		t.Fatalf("LiveStatuses = %v", LiveStatuses())
	}
}

func TestAnEditSendsAReadyCampaignBackToDraft(t *testing.T) {
	cases := map[Status]Status{StatusDraft: StatusDraft, StatusReady: StatusDraft, StatusScheduled: StatusScheduled}
	for from, want := range cases {
		if got := from.AfterEdit(); got != want {
			t.Errorf("%q.AfterEdit() = %q, want %q", from, got, want)
		}
	}
	for _, s := range EditableStatuses() {
		if !s.Editable() {
			t.Errorf("EditableStatuses has %q", s)
		}
	}
	for _, s := range DeletableStatuses() {
		if !s.Deletable() {
			t.Errorf("DeletableStatuses has %q", s)
		}
	}
}
