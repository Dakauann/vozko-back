package billing

import (
	"testing"
	"time"
)

// ut builds a UTC date at midnight. Production passes America/Sao_Paulo, but the pure date math
// is location-agnostic for these functions and Brazil no longer observes DST, so UTC keeps the
// tests deterministic and free of any tzdata dependency on the build host.
func ut(year int, month time.Month, day int) time.Time {
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}

// utt builds a UTC instant at a specific hour:minute.
func utt(year int, month time.Month, day, hour, min int) time.Time {
	return time.Date(year, month, day, hour, min, 0, 0, time.UTC)
}

func TestPlanFirstAnchor(t *testing.T) {
	cases := []struct {
		name      string
		purchase  time.Time
		dueDay    int
		floorDays int
		want      time.Time
	}{
		{
			// Plan section 10.9: purchase day 2, the 23rd is 21 days away, above the floor.
			name:     "early purchase, same month anchor",
			purchase: ut(2026, time.January, 2),
			dueDay:   23, floorDays: 10,
			want: ut(2026, time.January, 23),
		},
		{
			// Day 20 to the 23rd is only 3 days, below the floor, so roll to next month.
			name:     "late purchase rolls past the floor",
			purchase: ut(2026, time.January, 20),
			dueDay:   23, floorDays: 10,
			want: ut(2026, time.February, 23),
		},
		{
			// Exactly floorDays away (10 days) must qualify (boundary is inclusive).
			name:     "exactly on the floor qualifies",
			purchase: ut(2026, time.January, 13),
			dueDay:   23, floorDays: 10,
			want: ut(2026, time.January, 23),
		},
		{
			// One day short of the floor rolls forward.
			name:     "one day short of floor rolls",
			purchase: ut(2026, time.January, 14),
			dueDay:   23, floorDays: 10,
			want: ut(2026, time.February, 23),
		},
		{
			// Purchase exactly on the anchor day: not strictly after, so roll to next month.
			name:     "purchase on the anchor day rolls",
			purchase: ut(2026, time.January, 23),
			dueDay:   23, floorDays: 10,
			want: ut(2026, time.February, 23),
		},
		{
			// dueDay 31 in a 30-day month clamps to the last day.
			name:     "dueDay clamps in a short month",
			purchase: ut(2026, time.April, 1),
			dueDay:   31, floorDays: 10,
			want: ut(2026, time.April, 30),
		},
		{
			// Year boundary: December purchase rolls into next January.
			name:     "rolls across the year boundary",
			purchase: ut(2026, time.December, 20),
			dueDay:   23, floorDays: 10,
			want: ut(2027, time.January, 23),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := PlanFirstAnchor(tc.purchase, tc.dueDay, tc.floorDays)
			if !got.Equal(tc.want) {
				t.Fatalf("PlanFirstAnchor(%s, %d, %d) = %s, want %s",
					tc.purchase.Format("2006-01-02"), tc.dueDay, tc.floorDays,
					got.Format("2006-01-02"), tc.want.Format("2006-01-02"))
			}
		})
	}
}

func TestNextAnchor(t *testing.T) {
	cases := []struct {
		name   string
		from   time.Time
		dueDay int
		want   time.Time
	}{
		{
			name: "before anchor day, same month",
			from: ut(2026, time.January, 10), dueDay: 23,
			want: ut(2026, time.January, 23),
		},
		{
			name: "after anchor day, next month",
			from: ut(2026, time.January, 25), dueDay: 23,
			want: ut(2026, time.February, 23),
		},
		{
			name: "exactly on anchor midnight returns same day",
			from: ut(2026, time.January, 23), dueDay: 23,
			want: ut(2026, time.January, 23),
		},
		{
			name: "anchor day but later than midnight rolls forward",
			from: utt(2026, time.January, 23, 10, 0), dueDay: 23,
			want: ut(2026, time.February, 23),
		},
		{
			name: "dueDay 31 clamps in February (non-leap)",
			from: ut(2026, time.February, 1), dueDay: 31,
			want: ut(2026, time.February, 28),
		},
		{
			name: "dueDay 31 clamps in February (leap)",
			from: ut(2024, time.February, 1), dueDay: 31,
			want: ut(2024, time.February, 29),
		},
		{
			name: "rolls across the year boundary",
			from: ut(2026, time.December, 25), dueDay: 23,
			want: ut(2027, time.January, 23),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := NextAnchor(tc.from, tc.dueDay)
			if !got.Equal(tc.want) {
				t.Fatalf("NextAnchor(%s, %d) = %s, want %s",
					tc.from.Format(time.RFC3339), tc.dueDay,
					got.Format(time.RFC3339), tc.want.Format(time.RFC3339))
			}
		})
	}
}

func TestCancelCutoff(t *testing.T) {
	end := func(y int, m time.Month, d int) time.Time {
		return time.Date(y, m, d, 23, 59, 59, 0, time.UTC)
	}
	cases := []struct {
		name      string
		month     time.Time
		cutoffDay int
		want      time.Time
	}{
		{
			name:  "february cutoff is day 27 (the tight month)",
			month: ut(2026, time.February, 1), cutoffDay: 27,
			want: end(2026, time.February, 27),
		},
		{
			name:  "leap february cutoff is still day 27",
			month: ut(2024, time.February, 15), cutoffDay: 27,
			want: end(2024, time.February, 27),
		},
		{
			name:  "thirty-one day month, day 27",
			month: ut(2026, time.January, 9), cutoffDay: 27,
			want: end(2026, time.January, 27),
		},
		{
			name:  "cutoff day clamps when it exceeds the month length",
			month: ut(2026, time.February, 1), cutoffDay: 31,
			want: end(2026, time.February, 28),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := CancelCutoff(tc.month, time.UTC, tc.cutoffDay)
			if !got.Equal(tc.want) {
				t.Fatalf("CancelCutoff(%s, UTC, %d) = %s, want %s",
					tc.month.Format("2006-01"), tc.cutoffDay,
					got.Format(time.RFC3339), tc.want.Format(time.RFC3339))
			}
		})
	}
}
