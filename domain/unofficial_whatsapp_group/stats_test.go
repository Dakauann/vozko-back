package unofficial_whatsapp_group

import (
	"errors"
	"testing"
	"time"
)

func TestResolveWindowCoversWholeLocalDays(t *testing.T) {
	loc, _ := time.LoadLocation("America/Sao_Paulo")
	now := time.Date(2026, 10, 17, 10, 30, 0, 0, loc)

	cases := []struct {
		period   Period
		wantFrom time.Time
	}{
		{PeriodMonth, time.Date(2026, 10, 1, 0, 0, 0, 0, loc)},
		{Period7d, time.Date(2026, 10, 11, 0, 0, 0, 0, loc)},
		{Period30d, time.Date(2026, 9, 18, 0, 0, 0, 0, loc)},
		{Period90d, time.Date(2026, 7, 20, 0, 0, 0, 0, loc)},
	}
	for _, c := range cases {
		w, err := ResolveWindow(c.period, now, loc)
		if err != nil {
			t.Fatalf("%s: %v", c.period, err)
		}
		if !w.From.Equal(c.wantFrom) {
			t.Fatalf("%s from = %v, want %v", c.period, w.From.In(loc), c.wantFrom)
		}
		if !w.To.Equal(now) {
			t.Fatalf("%s to = %v, want now", c.period, w.To)
		}
	}
	if _, err := ResolveWindow("year", now, loc); !errors.Is(err, ErrPeriodInvalid) {
		t.Fatalf("unknown period error = %v", err)
	}
}

func TestWindowDaysListsEveryLocalDay(t *testing.T) {
	loc, _ := time.LoadLocation("America/Sao_Paulo")
	now := time.Date(2026, 10, 17, 10, 0, 0, 0, loc)
	w, _ := ResolveWindow(Period7d, now, loc)
	days := w.Days(loc)
	if len(days) != 7 {
		t.Fatalf("days = %d, want 7", len(days))
	}
	if days[0].Format("2006-01-02") != "2026-10-11" || days[6].Format("2006-01-02") != "2026-10-17" {
		t.Fatalf("days = %v .. %v", days[0], days[6])
	}
}

func TestChurnRateNeedsABaseOfMembers(t *testing.T) {
	if got := ChurnRate(5, 0); got != nil {
		t.Fatalf("churn with no starting members = %v, want no base", *got)
	}
	got := ChurnRate(25, 287)
	if got == nil || *got < 8.7 || *got > 8.72 {
		t.Fatalf("churn = %v, want about 8.71%%", got)
	}
}

func TestFillDailyZeroFillsMissingDays(t *testing.T) {
	loc := time.UTC
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, loc)
	w := Window{From: time.Date(2026, 10, 1, 0, 0, 0, 0, loc), To: now}
	points := FillDaily(w, loc, map[string]DailyCount{
		"2026-10-02": {Messages: 7, ActiveGroups: 2, Joins: 3, Leaves: 1},
	})
	if len(points) != 3 {
		t.Fatalf("points = %d, want 3", len(points))
	}
	if points[0].Messages != 0 || points[1].Messages != 7 || points[1].Net != 2 || points[2].Joins != 0 {
		t.Fatalf("points = %+v", points)
	}
}

func TestDayKeyUsesTheWorkspaceZone(t *testing.T) {
	loc, _ := time.LoadLocation("America/Sao_Paulo")
	lateUTC := time.Date(2026, 10, 18, 1, 0, 0, 0, time.UTC)
	if got := DayKey(lateUTC, loc); got != "2026-10-17" {
		t.Fatalf("DayKey = %s, want the local day 2026-10-17", got)
	}
}

func TestFillResponseTimesKeepsEmptyDaysAtZero(t *testing.T) {
	w := Window{From: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 10, 3, 9, 0, 0, 0, time.UTC)}
	points := FillResponseTimes(w, time.UTC, map[string]ResponseTimePoint{"2026-10-02": {AvgMinutes: 4.5, Samples: 9}})
	if len(points) != 3 || points[0].Samples != 0 || points[1].AvgMinutes != 4.5 || !points[2].Day.Equal(time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("points = %+v", points)
	}
}

func TestGrowthRateNeedsABaseOfMembers(t *testing.T) {
	if GrowthRate(5, 0) != nil {
		t.Fatalf("growth without a base must have no value")
	}
	if got := GrowthRate(-5, 50); got == nil || *got != -10 {
		t.Fatalf("growth = %v, want -10", got)
	}
}

func TestTheCrossGroupMinimumIsAtLeastTwo(t *testing.T) {
	for in, want := range map[int]int{0: MinCrossGroups, 1: MinCrossGroups, 2: 2, 5: 5} {
		if got := CrossGroupMinimum(in); got != want {
			t.Fatalf("CrossGroupMinimum(%d) = %d, want %d", in, got, want)
		}
	}
}
