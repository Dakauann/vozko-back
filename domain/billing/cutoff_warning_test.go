package billing

import (
	"testing"
	"time"
)

func TestTheCutoffWarningRunsFromThePastDueDayUntilTheCutoff(t *testing.T) {
	loc := LocationBRT()
	due := time.Date(2026, 3, 23, 0, 0, 0, 0, loc)
	cutoff := CancelCutoff(due, loc, DefaultCancelDeadlineDay)
	cases := map[string]struct {
		now  time.Time
		want bool
	}{
		"before the due date": {time.Date(2026, 3, 22, 12, 0, 0, 0, loc), false},
		"on the due date":     {time.Date(2026, 3, 23, 18, 0, 0, 0, loc), false},
		"the day after due":   {time.Date(2026, 3, 24, 9, 0, 0, 0, loc), true},
		"on the cutoff day":   {time.Date(2026, 3, 27, 12, 0, 0, 0, loc), true},
		"after the cutoff":    {time.Date(2026, 3, 28, 0, 1, 0, 0, loc), false},
	}
	for name, tc := range cases {
		if got := InCutoffWarning(tc.now, due, cutoff); got != tc.want {
			t.Errorf("%s: got %v, want %v", name, got, tc.want)
		}
	}
}
