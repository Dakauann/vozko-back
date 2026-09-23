package attendance_target

import (
	"testing"
	"time"
)

func fortaleza(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("America/Fortaleza")
	if err != nil {
		t.Skip("tzdata unavailable on this machine")
	}
	return loc
}

func TestParsedMonthDoesNotRollBackInANegativeOffsetZone(t *testing.T) {
	loc := fortaleza(t)

	month, ok := ParseMonth("2026-09")
	if !ok {
		t.Fatal("2026-09 must parse")
	}

	start := month.Start(loc)
	if start.Year() != 2026 || start.Month() != time.September || start.Day() != 1 {
		t.Fatalf("start = %s, want the first of September in the workspace zone", start)
	}
	if _, offset := start.Zone(); offset != -3*3600 {
		t.Fatalf("offset = %d, want the workspace zone", offset)
	}
}

func TestTheCurrentMonthIsNotClosed(t *testing.T) {
	loc := fortaleza(t)

	month, _ := ParseMonth("2026-09")
	target := Target{PeriodStart: month.Start(loc)}

	now := time.Date(2026, time.September, 23, 12, 0, 0, 0, loc)
	if target.PeriodHasClosed(now) {
		t.Fatal("September is still open on the 23rd; goals must stay editable")
	}
}

func TestAMonthClosesTheInstantItEnds(t *testing.T) {
	loc := fortaleza(t)

	month, _ := ParseMonth("2026-09")
	target := Target{PeriodStart: month.Start(loc)}

	lastMoment := time.Date(2026, time.September, 30, 23, 59, 59, 0, loc)
	if target.PeriodHasClosed(lastMoment) {
		t.Fatal("September must stay open through its final second")
	}

	firstOfOctober := time.Date(2026, time.October, 1, 0, 0, 0, 0, loc)
	if !target.PeriodHasClosed(firstOfOctober) {
		t.Fatal("September must be closed once October starts locally")
	}
}

func TestMonthAtReadsTheLocalMonthNotTheUTCOne(t *testing.T) {
	loc := fortaleza(t)

	// One hour past midnight UTC on the 1st is still the previous month locally.
	instant := time.Date(2026, time.September, 1, 1, 0, 0, 0, time.UTC)

	month := MonthAt(instant, loc)
	if month.Month != time.August || month.Year != 2026 {
		t.Fatalf("month = %s, want 2026-08: locally it is still August", month)
	}
}

func TestAnEmptyPeriodMeansTheCurrentMonth(t *testing.T) {
	month, ok := ParseMonth("   ")
	if !ok {
		t.Fatal("an empty period is allowed; it means the current month")
	}
	if !month.IsZero() {
		t.Fatalf("month = %s, want the zero value that the service resolves", month)
	}
}

func TestParseMonthRejectsRubbish(t *testing.T) {
	for _, raw := range []string{"2026", "september", "2026-13-01", "26-09"} {
		if _, ok := ParseMonth(raw); ok {
			t.Fatalf("%q must not parse as a month", raw)
		}
	}
}

func TestMonthRoundTripsThroughItsString(t *testing.T) {
	month, _ := ParseMonth("2026-09")
	if month.String() != "2026-09" {
		t.Fatalf("String() = %q", month.String())
	}

	again, ok := ParseMonth(month.String())
	if !ok || again != month {
		t.Fatalf("round trip gave %v (ok=%v)", again, ok)
	}
}
