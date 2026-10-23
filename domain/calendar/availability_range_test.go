package calendar

import (
	"errors"
	"testing"
	"time"
)

func saoPaulo(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

func TestAvailabilityRangeAcceptsDatesAndRequiresAValidInterval(t *testing.T) {
	loc := saoPaulo(t)
	for _, tc := range []struct {
		name, from, to, start, end string
		valid                      bool
	}{
		{"same day from agent", "2026-10-23", "2026-10-23", "2026-10-23T08:00:00-03:00", "2026-10-23T18:00:00-03:00", true},
		{"default end", "2026-10-23", "", "2026-10-23T08:00:00-03:00", "2026-10-23T18:00:00-03:00", true},
		{"local timestamp", "2026-10-23T09:00:00", "", "2026-10-23T09:00:00-03:00", "2026-10-23T18:00:00-03:00", true},
		{"offset preserved", "2026-10-23T12:00:00Z", "2026-10-23T13:00:00Z", "2026-10-23T12:00:00Z", "2026-10-23T13:00:00Z", true},
		{"after close without end", "2026-10-23T19:00:00", "", "2026-10-23T19:00:00-03:00", "2026-10-24T00:00:00-03:00", true},
		{"equal timestamps", "2026-10-23T09:00:00", "2026-10-23T09:00:00", "", "", false},
		{"backwards dates", "2026-10-24", "2026-10-23", "", "", false},
		{"ambiguous date", "23/10/2026", "", "", "", false},
		{"words", "amanhã", "", "", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			start, end, err := AvailabilityRange(tc.from, tc.to, DefaultWorkStart, DefaultWorkEnd, loc)
			if (err == nil) != tc.valid {
				t.Fatalf("range error = %v", err)
			}
			if tc.valid && (start.Format(time.RFC3339) != tc.start || end.Format(time.RFC3339) != tc.end) {
				t.Fatalf("range = %s to %s", start, end)
			}
		})
	}
}

func TestAvailabilityRangeUsesTheConfiguredWorkingHoursForWholeDays(t *testing.T) {
	loc := saoPaulo(t)
	start, end, err := AvailabilityRange("2026-10-24", "", ParseClock("09:30", DefaultWorkStart), ParseClock("12:00", DefaultWorkEnd), loc)
	if err != nil || start.Format(time.RFC3339) != "2026-10-24T09:30:00-03:00" || end.Format(time.RFC3339) != "2026-10-24T12:00:00-03:00" {
		t.Fatalf("range = %s to %s, %v", start, end, err)
	}
}

func TestParseClockFallsBackOnMissingOrInvalidValues(t *testing.T) {
	for raw, want := range map[string]Clock{"": DefaultWorkStart, "abc": DefaultWorkStart, "25:00": DefaultWorkStart, "8:15": {Hour: 8, Minute: 15}, " 13:45 ": {Hour: 13, Minute: 45}} {
		if got := ParseClock(raw, DefaultWorkStart); got != want {
			t.Fatalf("ParseClock(%q) = %+v, want %+v", raw, got, want)
		}
	}
}

func TestParseDateTimeRequiresATime(t *testing.T) {
	loc := saoPaulo(t)
	if _, err := ParseDateTime("2026-10-23", loc); !errors.Is(err, ErrDateTimeFormat) {
		t.Fatalf("date without time accepted: %v", err)
	}
	at, err := ParseDateTime("2026-10-23 14:00", loc)
	if err != nil || at.Format(time.RFC3339) != "2026-10-23T14:00:00-03:00" {
		t.Fatalf("local time = %s, %v", at, err)
	}
}
