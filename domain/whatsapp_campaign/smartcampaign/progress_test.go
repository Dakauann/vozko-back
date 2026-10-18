package smartcampaign

import (
	"errors"
	"testing"
	"time"

	"vozko/domain/campaign"
)

func TestDescribeSaysWhatHappensNext(t *testing.T) {
	inFlight := &Wave{Number: 3, State: Enqueued}
	observing := &Wave{Number: 3, State: Drained, ObserveUntil: noon.Add(2 * time.Hour)}
	decided := &Wave{Number: 3, State: Decided}
	hold := &Hold{Decision: Decision{Action: Pause}, At: noon}
	waiting := &Decision{Action: Wait, WaitUntil: noon.Add(20 * time.Hour)}

	cases := []struct {
		name    string
		status  campaign.Status
		state   State
		current *Wave
		phase   Phase
		until   time.Time
		wave    int
	}{
		{"never started", campaign.StatusStopped, State{}, nil, PhaseNotStarted, time.Time{}, 0},
		{"a wave is going out", campaign.StatusRunning, State{}, inFlight, PhaseSending, time.Time{}, 3},
		{"a wave is being observed", campaign.StatusRunning, State{}, observing, PhaseObserving, noon.Add(2 * time.Hour), 3},
		{"waiting for the send hours", campaign.StatusRunning, State{Waiting: waiting, DueAt: noon.Add(20 * time.Hour)}, observing, PhaseWaiting, noon.Add(20 * time.Hour), 3},
		{"about to release the next wave", campaign.StatusRunning, State{DueAt: noon}, decided, PhaseSending, noon, 4},
		{"held automatically", campaign.StatusPaused, State{Hold: hold}, decided, PhaseHeld, time.Time{}, 3},
		{"paused by a person", campaign.StatusPaused, State{}, inFlight, PhasePaused, time.Time{}, 3},
		{"stopped by a person", campaign.StatusStopped, State{}, inFlight, PhaseStopped, time.Time{}, 3},
		{"completed", campaign.StatusCompleted, State{}, decided, PhaseCompleted, time.Time{}, 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Describe(tc.status, tc.state, tc.current)
			if got.Phase != tc.phase || !got.Until.Equal(tc.until) || got.Wave != tc.wave {
				t.Fatalf("Describe = %+v, want phase %s until %v wave %d", got, tc.phase, tc.until, tc.wave)
			}
		})
	}
}

func TestHeldAndWaitingCarryTheirReason(t *testing.T) {
	hold := &Hold{Decision: Decision{Action: Stop, Findings: []Finding{{Rule: RuleSpamLimit, Action: Stop}}}, At: noon}
	if got := Describe(campaign.StatusStopped, State{Hold: hold}, nil); got.Reason == nil || got.Reason.Action != Stop || !got.Since.Equal(noon) {
		t.Fatalf("held progress = %+v, want the hold decision and when it happened", got)
	}
	waiting := &Decision{Action: Wait}
	if got := Describe(campaign.StatusRunning, State{Waiting: waiting}, nil); got.Reason != waiting {
		t.Fatalf("waiting progress = %+v, want the waiting decision", got)
	}
}

func TestBlockedErrorExplainsItself(t *testing.T) {
	err := error(&BlockedError{Decision: Decision{Action: Pause, Findings: []Finding{{Rule: RuleTemplateYellow, Action: Pause}}}})
	var blocked *BlockedError
	if !errors.Is(err, ErrStillUnhealthy) || !errors.As(err, &blocked) || blocked.Decision.Action != Pause {
		t.Fatalf("blocked error = %v, want it to unwrap to ErrStillUnhealthy and keep the decision", err)
	}
}
