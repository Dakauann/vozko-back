package calendar

import (
	"testing"
	"time"
)

func TestAvailabilityRangeAcceptsDatesAndRequiresAValidInterval(t *testing.T) {
	loc, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, from, to, start, end string
		valid                      bool
	}{
		{"same day from agent", "2026-10-23", "2026-10-23", "2026-10-23T08:00:00-03:00", "2026-10-23T18:00:00-03:00", true},
		{"default end", "2026-10-23", "", "2026-10-23T08:00:00-03:00", "2026-10-23T18:00:00-03:00", true},
		{"local timestamp", "2026-10-23T09:00:00", "", "2026-10-23T09:00:00-03:00", "2026-10-23T18:00:00-03:00", true},
		{"offset preserved", "2026-10-23T12:00:00Z", "2026-10-23T13:00:00Z", "2026-10-23T12:00:00Z", "2026-10-23T13:00:00Z", true},
		{"equal timestamps", "2026-10-23T09:00:00", "2026-10-23T09:00:00", "", "", false},
		{"backwards dates", "2026-10-24", "2026-10-23", "", "", false},
		{"ambiguous date", "23/10/2026", "", "", "", false},
		{"after close", "2026-10-23T19:00:00", "", "", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			start, end, err := AvailabilityRange(tc.from, tc.to, "08:00", "18:00", loc)
			if (err == nil) != tc.valid {
				t.Fatalf("range error = %v", err)
			}
			if tc.valid && (start.Format(time.RFC3339) != tc.start || end.Format(time.RFC3339) != tc.end) {
				t.Fatalf("range = %s to %s", start, end)
			}
		})
	}
}
