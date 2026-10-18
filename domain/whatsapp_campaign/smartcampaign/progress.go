package smartcampaign

import (
	"strings"
	"time"

	"vozko/domain/campaign"
)

type Phase string

const (
	PhaseNotStarted Phase = "not_started"
	PhaseSending    Phase = "sending"
	PhaseObserving  Phase = "observing"
	PhaseWaiting    Phase = "waiting"
	PhaseHeld       Phase = "held"
	PhasePaused     Phase = "paused"
	PhaseStopped    Phase = "stopped"
	PhaseCompleted  Phase = "completed"
)

type Progress struct {
	Phase  Phase     `json:"phase"`
	Wave   int       `json:"wave,omitempty"`
	Until  time.Time `json:"until,omitempty"`
	Since  time.Time `json:"since,omitempty"`
	Reason *Decision `json:"reason,omitempty"`
}

func Describe(status campaign.Status, state State, current *Wave) Progress {
	number := 0
	if current != nil {
		number = current.Number
	}
	switch {
	case status == campaign.StatusCompleted:
		return Progress{Phase: PhaseCompleted, Wave: number}
	case state.Hold != nil && status != campaign.StatusRunning:
		return Progress{Phase: PhaseHeld, Wave: number, Since: state.Hold.At, Reason: &state.Hold.Decision}
	case status == campaign.StatusPaused:
		return Progress{Phase: PhasePaused, Wave: number}
	case status == campaign.StatusStopped && current == nil:
		return Progress{Phase: PhaseNotStarted}
	case status == campaign.StatusStopped:
		return Progress{Phase: PhaseStopped, Wave: number}
	case state.Waiting != nil:
		return Progress{Phase: PhaseWaiting, Wave: number, Until: state.DueAt, Reason: state.Waiting}
	case current == nil:
		return Progress{Phase: PhaseSending, Wave: 1, Until: state.DueAt}
	case current.InFlight():
		return Progress{Phase: PhaseSending, Wave: number}
	case current.State == Drained:
		return Progress{Phase: PhaseObserving, Wave: number, Until: current.ObserveUntil}
	default:
		return Progress{Phase: PhaseSending, Wave: number + 1, Until: state.DueAt}
	}
}

type BlockedError struct {
	Decision Decision
}

func (e *BlockedError) Error() string {
	rules := make([]string, 0, len(e.Decision.Findings))
	for _, f := range e.Decision.Drivers() {
		rules = append(rules, string(f.Rule))
	}
	return ErrStillUnhealthy.Error() + ": " + strings.Join(rules, ", ")
}

func (e *BlockedError) Unwrap() error {
	return ErrStillUnhealthy
}
