package smartcampaign

import (
	"testing"
	"time"
)

func TestHealthReadsStartTheClockOnceAndClearItOnSuccess(t *testing.T) {
	var s State
	s.TrackHealthReads(true, noon)
	s.TrackHealthReads(true, noon.Add(time.Hour))
	if !s.UnreadSince.Equal(noon) {
		t.Fatalf("unread since = %v, want the first failed read", s.UnreadSince)
	}
	s.TrackHealthReads(false, noon.Add(2*time.Hour))
	if !s.UnreadSince.IsZero() {
		t.Fatal("a successful read must clear the clock")
	}
}

func TestResumingAfterAHoldRestartsSmall(t *testing.T) {
	var s State
	s.Held(Decision{Action: Pause}, noon)
	if s.Hold == nil || !s.DueAt.IsZero() {
		t.Fatal("a held campaign keeps its hold and is never due")
	}
	s.Resumed(noon.Add(time.Hour))
	if s.Hold != nil || !s.ResumedAfterHold || !s.DueAt.Equal(noon.Add(time.Hour)) {
		t.Fatalf("after resume: %+v", s)
	}
	s.WaveReleased(noon.Add(2 * time.Hour))
	if s.ResumedAfterHold {
		t.Fatal("the small restart applies to one wave only")
	}
}

func TestAResumeWithoutAHoldKeepsTheNormalSize(t *testing.T) {
	var s State
	s.Resumed(noon)
	if s.ResumedAfterHold {
		t.Fatal("a manual pause does not shrink the next wave")
	}
}

func TestWaitingKeepsTheReasonAndTheTime(t *testing.T) {
	var s State
	opens := noon.Add(20 * time.Hour)
	s.WaitFor(Decision{Action: Wait, WaitUntil: opens}, opens)
	if s.Waiting == nil || !s.DueAt.Equal(opens) {
		t.Fatalf("waiting state = %+v", s)
	}
	h := s.History(400, 1)
	if h.CurrentSize != 400 || h.SlowStreakAtFloor != 1 {
		t.Fatalf("history = %+v", h)
	}
}
