package advertising

import (
	"errors"
	"testing"
	"time"
)

func TestDateRangeCountsBothEnds(t *testing.T) {
	r, err := NewDateRange("2026-09-01", "2026-09-30")
	if err != nil || r.Days() != 30 {
		t.Fatalf("days = %d, %v", r.Days(), err)
	}
}

func TestBackwardsOrHugeRangesAreRefused(t *testing.T) {
	for _, pair := range [][2]string{{"2026-09-30", "2026-09-01"}, {"2020-01-01", "2026-01-01"}, {"x", "2026-01-01"}} {
		if _, err := NewDateRange(pair[0], pair[1]); !errors.Is(err, ErrInvalidRange) {
			t.Fatalf("%v accepted: %v", pair, err)
		}
	}
}

func TestDaysFollowTheAdAccountTimezoneNotTheServer(t *testing.T) {
	saoPaulo, _ := time.LoadLocation("America/Sao_Paulo")
	lateNightUTC := time.Date(2026, 10, 2, 1, 30, 0, 0, time.UTC)
	r := LastDays(7, lateNightUTC, saoPaulo)
	if got := r.Until.Format(DayLayout); got != "2026-10-01" {
		t.Fatalf("until = %s, want the São Paulo day", got)
	}
	if got := r.Since.Format(DayLayout); got != "2026-09-25" {
		t.Fatalf("since = %s", got)
	}
}

func TestBoundsCoverWholeLocalDays(t *testing.T) {
	saoPaulo, _ := time.LoadLocation("America/Sao_Paulo")
	r, _ := NewDateRange("2026-10-01", "2026-10-01")
	start, end := r.Bounds(saoPaulo)
	if start.UTC().Hour() != 3 || end.Sub(start) != 24*time.Hour {
		t.Fatalf("bounds %s to %s", start, end)
	}
}

func TestRangesReachBackThirtySevenMonthsLikeMeta(t *testing.T) {
	until := time.Date(2026, 3, 31, 0, 0, 0, 0, time.UTC)
	if got := MonthsBefore(until, 1); !got.Equal(time.Date(2026, 2, 28, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("month end must clamp, got %s", got.Format(DayLayout))
	}
	oldest := MonthsBefore(until, MaxRangeMonths)
	if !oldest.Equal(time.Date(2023, 2, 28, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("got %s", oldest.Format(DayLayout))
	}
	if err := (DateRange{Since: oldest, Until: until}).Validate(); err != nil {
		t.Fatalf("the oldest day Meta keeps must be accepted: %v", err)
	}
	if err := (DateRange{Since: oldest.AddDate(0, 0, -1), Until: until}).Validate(); err == nil {
		t.Fatal("a day older than 37 months must be refused")
	}
}
