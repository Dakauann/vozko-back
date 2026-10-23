package workflow

import (
	"testing"
	"time"
)

func TestRunState_GetInt_SurvivesJSONRoundTrip(t *testing.T) {
	orig := NewRunState()
	orig.Set(StateKeyDurableSteps, 4321)

	data, err := orig.JSON()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	reloaded, err := RunStateFromJSON(data)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got := reloaded.GetInt(StateKeyDurableSteps); got != 4321 {
		t.Fatalf("durable counter lost across JSON round-trip: got %d, want 4321", got)
	}
}

func TestRunState_GetInt_Variants(t *testing.T) {
	s := NewRunState()
	s.Set("i", 7)
	s.Set("i64", int64(9))
	s.Set("f", float64(11))
	s.Set("str", "nope")
	cases := map[string]int{"i": 7, "i64": 9, "f": 11, "str": 0, "missing": 0}
	for key, want := range cases {
		if got := s.GetInt(key); got != want {
			t.Errorf("GetInt(%q) = %d, want %d", key, got, want)
		}
	}
}

func TestOnlyAMessageSentAfterTheQuestionAnswersTheWait(t *testing.T) {
	run := &WorkflowRun{}
	run.SetWaiting(0, WaitReasonReply)
	asked := *run.WaitingSince

	for name, tc := range map[string]struct {
		sentAt time.Time
		want   bool
	}{
		"sent after the question":            {asked.Add(4 * time.Second), true},
		"same second, clock truncated":       {asked.Add(-900 * time.Millisecond), true},
		"sent before the question was asked": {asked.Add(-44 * time.Second), false},
		"a channel that reports no time":     {time.Time{}, true},
	} {
		if got := run.AnswersWait(tc.sentAt); got != tc.want {
			t.Errorf("%s: AnswersWait = %v, want %v", name, got, tc.want)
		}
	}

	run.SetRunning()
	if run.WaitingSince != nil || !run.AnswersWait(asked.Add(-time.Hour)) {
		t.Fatal("a running run is not waiting for anything")
	}
}
