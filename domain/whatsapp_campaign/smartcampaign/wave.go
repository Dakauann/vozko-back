package smartcampaign

import (
	"errors"
	"time"
)

type WaveState string

const (
	Released WaveState = "released"
	Enqueued WaveState = "enqueued"
	Drained  WaveState = "drained"
	Decided  WaveState = "decided"
)

var (
	ErrAlreadyReleased = errors.New("smart campaign: wave already released")
	ErrWaveTransition  = errors.New("smart campaign: wave cannot move to that state")
)

type Wave struct {
	CampaignID   string    `json:"campaignId"`
	Number       int       `json:"number"`
	Size         int       `json:"size"`
	State        WaveState `json:"state"`
	ReleasedAt   time.Time `json:"releasedAt"`
	DrainedAt    time.Time `json:"drainedAt,omitzero"`
	ObserveUntil time.Time `json:"observeUntil,omitzero"`
	DecidedAt    time.Time `json:"decidedAt,omitzero"`
	Outcome      Outcome   `json:"outcome"`
	Decision     Decision  `json:"decision"`
}

func Release(campaignID string, number, size int, at time.Time) Wave {
	return Wave{CampaignID: campaignID, Number: number, Size: size, State: Released, ReleasedAt: at}
}

func (w *Wave) MarkEnqueued() error {
	if w.State != Released {
		return ErrWaveTransition
	}
	w.State = Enqueued
	return nil
}

func (w *Wave) MarkDrained(at time.Time, observe time.Duration) error {
	if w.State != Enqueued {
		return ErrWaveTransition
	}
	w.State = Drained
	w.DrainedAt = at
	w.ObserveUntil = at.Add(observe)
	return nil
}

func (w *Wave) MarkDecided(at time.Time, outcome Outcome, decision Decision) {
	w.State = Decided
	w.DecidedAt = at
	w.Outcome = outcome
	w.Decision = decision
}

func (w Wave) InFlight() bool {
	return w.State == Released || w.State == Enqueued
}

func (w Wave) Judged() bool {
	return w.State == Decided
}

func (w Wave) ObservationOver(now time.Time) bool {
	return w.State == Drained && !now.Before(w.ObserveUntil)
}

func SlowStreakAtFloor(waves []Wave, floor int) int {
	streak := 0
	for i := len(waves) - 1; i >= 0; i-- {
		w := waves[i]
		if !w.Judged() {
			continue
		}
		if w.Decision.Action != SlowDown || w.Size > floor {
			break
		}
		streak++
	}
	return streak
}
