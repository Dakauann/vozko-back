package smartcampaign

import (
	"errors"
	"testing"
	"time"
)

func TestWaveMovesForwardOnly(t *testing.T) {
	w := Release("camp", 1, 200, noon)
	if w.State != Released {
		t.Fatalf("new wave state = %s, want released", w.State)
	}
	if err := w.MarkDrained(noon, time.Hour); !errors.Is(err, ErrWaveTransition) {
		t.Fatalf("draining a released wave = %v, want ErrWaveTransition", err)
	}
	if err := w.MarkEnqueued(); err != nil {
		t.Fatal(err)
	}
	if err := w.MarkDrained(noon, 2*time.Hour); err != nil {
		t.Fatal(err)
	}
	if !w.ObserveUntil.Equal(noon.Add(2 * time.Hour)) {
		t.Fatalf("observe until = %v", w.ObserveUntil)
	}
	if w.ObservationOver(noon.Add(time.Hour)) || !w.ObservationOver(noon.Add(2*time.Hour)) {
		t.Fatal("the observation must end exactly at observe until")
	}
	w.MarkDecided(noon.Add(2*time.Hour), Outcome{Processed: 200}, Decision{Action: Continue})
	if !w.Judged() || w.Outcome.Processed != 200 {
		t.Fatal("a decided wave keeps its outcome and is judged")
	}
	if err := w.MarkEnqueued(); !errors.Is(err, ErrWaveTransition) {
		t.Fatalf("enqueueing a decided wave = %v, want ErrWaveTransition", err)
	}
}

func TestInFlightWavesAreReleasedOrEnqueued(t *testing.T) {
	cases := map[WaveState]bool{Released: true, Enqueued: true, Drained: false, Decided: false}
	for state, want := range cases {
		if got := (Wave{State: state}).InFlight(); got != want {
			t.Fatalf("%s in flight = %v, want %v", state, got, want)
		}
	}
}

func TestSlowStreakCountsOnlyTrailingSlowWavesAtTheFloor(t *testing.T) {
	judged := func(size int, action Action) Wave {
		return Wave{Size: size, State: Decided, Decision: Decision{Action: action}}
	}
	waves := []Wave{
		judged(50, SlowDown), judged(100, Continue), judged(50, SlowDown), judged(50, SlowDown),
		{Size: 50, State: Drained},
	}
	if got := SlowStreakAtFloor(waves, 50); got != 2 {
		t.Fatalf("streak = %d, want 2", got)
	}
	if got := SlowStreakAtFloor(nil, 50); got != 0 {
		t.Fatalf("empty streak = %d, want 0", got)
	}
}
