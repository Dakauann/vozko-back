package shared

import (
	"errors"
	"testing"
	"time"
)

func TestMonthlyCycleStart(t *testing.T) {
	brt := time.FixedZone("BRT", -3*3600)
	cases := []struct {
		name     string
		now      time.Time
		cycleDay int
		want     time.Time
	}{
		{"before the cycle day falls back a month", time.Date(2026, 10, 10, 9, 0, 0, 0, brt), 15, time.Date(2026, 9, 15, 0, 0, 0, 0, brt)},
		{"on the cycle day starts a new cycle", time.Date(2026, 10, 15, 0, 0, 0, 0, brt), 15, time.Date(2026, 10, 15, 0, 0, 0, 0, brt)},
		{"day 31 clamps to a short month", time.Date(2026, 2, 28, 12, 0, 0, 0, brt), 31, time.Date(2026, 2, 28, 0, 0, 0, 0, brt)},
		{"utc midnight is still the previous day locally", time.Date(2026, 10, 1, 1, 0, 0, 0, time.UTC), 1, time.Date(2026, 9, 1, 0, 0, 0, 0, brt)},
		{"a day below one reads as the first", time.Date(2026, 10, 5, 0, 0, 0, 0, brt), 0, time.Date(2026, 10, 1, 0, 0, 0, 0, brt)},
		{"january rolls the year back", time.Date(2027, 1, 3, 0, 0, 0, 0, brt), 10, time.Date(2026, 12, 10, 0, 0, 0, 0, brt)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := MonthlyCycleStart(tc.now, tc.cycleDay, brt); !got.Equal(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestMonthlyQuota(t *testing.T) {
	cases := []struct {
		name      string
		limit     int64
		used      int64
		remaining int64
		room      bool
	}{
		{"untouched", 100, 0, 100, true},
		{"one left", 100, 99, 1, true},
		{"at the limit", 100, 100, 0, false},
		{"over the limit", 100, 130, 0, false},
		{"refunds give room back", 100, -2, 102, true},
		{"an unset ceiling allows nothing", 0, 0, 0, false},
		{"a negative ceiling allows nothing", -5, 0, 0, false},
		{"refunds never open an unset ceiling", 0, -3, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			q := MonthlyQuota{Limit: tc.limit, CycleDay: 1}
			if got := q.Remaining(tc.used); got != tc.remaining {
				t.Errorf("Remaining = %d, want %d", got, tc.remaining)
			}
			err := q.CheckRoom(tc.used)
			if tc.room && err != nil {
				t.Errorf("want room, got %v", err)
			}
			if !tc.room && !errors.Is(err, ErrMonthlyQuotaReached) {
				t.Errorf("want ErrMonthlyQuotaReached, got %v", err)
			}
		})
	}
}

func TestMonthlyQuotaCycles(t *testing.T) {
	brt := time.FixedZone("BRT", -3*3600)
	q := MonthlyQuota{Limit: 10, CycleDay: 15, Location: brt}
	now := time.Date(2026, 10, 20, 9, 0, 0, 0, brt)
	if got := q.CycleStart(now); !got.Equal(time.Date(2026, 10, 15, 0, 0, 0, 0, brt)) {
		t.Fatalf("CycleStart = %v", got)
	}
	if got := q.NextCycleStart(now); !got.Equal(time.Date(2026, 11, 15, 0, 0, 0, 0, brt)) {
		t.Fatalf("NextCycleStart = %v", got)
	}
	if got := (MonthlyQuota{Limit: 1}).CycleStart(time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)); !got.Equal(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("a quota without a location counts in UTC, got %v", got)
	}
}
