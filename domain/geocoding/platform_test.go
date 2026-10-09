package geocoding

import (
	"math"
	"testing"
	"time"

	"vozko/domain/billing"
	"vozko/domain/leadmap"
)

func brtDate(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, billing.LocationBRT())
}

func TestPeriodAtFollowsTheBrasiliaCalendar(t *testing.T) {
	tests := []struct {
		name  string
		now   time.Time
		cycle time.Time
		next  time.Time
		day   time.Time
	}{
		{"mid month", time.Date(2026, 10, 8, 15, 0, 0, 0, time.UTC), brtDate(2026, 10, 1), brtDate(2026, 11, 1), brtDate(2026, 10, 8)},
		{"the first hours of a UTC month are still the previous Brasilia month", time.Date(2026, 11, 1, 2, 0, 0, 0, time.UTC), brtDate(2026, 10, 1), brtDate(2026, 11, 1), brtDate(2026, 10, 31)},
		{"january rolls back into december", time.Date(2027, 1, 1, 1, 0, 0, 0, time.UTC), brtDate(2026, 12, 1), brtDate(2027, 1, 1), brtDate(2026, 12, 31)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := PeriodAt(tt.now)
			if !p.CycleStart.Equal(tt.cycle) || !p.NextCycle.Equal(tt.next) || !p.Day.Equal(tt.day) || !p.NextDay.Equal(tt.day.AddDate(0, 0, 1)) {
				t.Fatalf("PeriodAt(%s) = %+v, want cycle %s to %s on day %s", tt.now, p, tt.cycle, tt.next, tt.day)
			}
		})
	}
}

func TestASlotUsesTheSamePeriodAsTheReport(t *testing.T) {
	now := time.Date(2026, 10, 8, 15, 0, 0, 0, time.UTC)
	slot, err := Settings{Provider: ProviderOpenCage}.SlotAt(now)
	if err != nil {
		t.Fatal(err)
	}
	p := PeriodAt(now)
	if !slot.CycleStart.Equal(p.CycleStart) || !slot.NextCycle.Equal(p.NextCycle) || !slot.Day.Equal(p.Day) || !slot.NextDay.Equal(p.NextDay) {
		t.Fatalf("SlotAt() = %+v, want the period %+v", slot, p)
	}
}

func TestHistoryCyclesAreTheLastTwelveMonthsNewestFirst(t *testing.T) {
	cycles := PeriodAt(time.Date(2026, 3, 10, 15, 0, 0, 0, time.UTC)).HistoryCycles()
	if len(cycles) != HistoryCycles {
		t.Fatalf("HistoryCycles() has %d cycles, want %d", len(cycles), HistoryCycles)
	}
	want := []time.Time{brtDate(2026, 3, 1), brtDate(2026, 2, 1), brtDate(2026, 1, 1), brtDate(2025, 12, 1)}
	for i, w := range want {
		if !cycles[i].Equal(w) {
			t.Fatalf("cycle %d = %s, want %s", i, cycles[i], w)
		}
	}
	if last := cycles[len(cycles)-1]; !last.Equal(brtDate(2025, 4, 1)) {
		t.Fatalf("oldest cycle = %s, want 2025-04-01", last)
	}
	if since := PeriodAt(time.Date(2026, 3, 10, 15, 0, 0, 0, time.UTC)).HistorySince(); !since.Equal(brtDate(2025, 4, 1)) {
		t.Fatalf("HistorySince() = %s, want the oldest cycle", since)
	}
}

func TestMonthsOfFillsEveryCycleAndIgnoresRowsOutsideTheWindow(t *testing.T) {
	p := PeriodAt(time.Date(2026, 10, 8, 15, 0, 0, 0, time.UTC))
	rows := []MonthUsage{
		{CycleStart: brtDate(2026, 10, 1).UTC(), Requests: 40},
		{CycleStart: brtDate(2026, 8, 1), Requests: 7},
		{CycleStart: brtDate(2026, 8, 1), Requests: 9},
		{CycleStart: brtDate(2024, 1, 1), Requests: 999},
	}
	months := MonthsOf(p, rows)
	if len(months) != HistoryCycles {
		t.Fatalf("MonthsOf() has %d months, want %d", len(months), HistoryCycles)
	}
	got := map[time.Time]int64{}
	var total int64
	for _, m := range months {
		got[m.CycleStart] = m.Requests
		total += m.Requests
	}
	if got[brtDate(2026, 10, 1)] != 40 || got[brtDate(2026, 9, 1)] != 0 || got[brtDate(2026, 8, 1)] != 9 {
		t.Fatalf("MonthsOf() = %+v, want 40 this cycle, 0 last cycle and the larger of two rows for August", months)
	}
	if total != 49 {
		t.Fatalf("MonthsOf() totals %d, want 49 without the row from before the window", total)
	}
	if !months[0].CycleStart.Equal(p.CycleStart) {
		t.Fatalf("the first month is %s, want the current cycle", months[0].CycleStart)
	}
}

func TestUsageAtCountsOnlyTheCurrentCycleAndDay(t *testing.T) {
	p := PeriodAt(time.Date(2026, 10, 8, 15, 0, 0, 0, time.UTC))
	tests := []struct {
		name     string
		usage    Usage
		requests int64
		today    int64
	}{
		{"this cycle and today", Usage{CycleStart: p.CycleStart, Requests: 12, Day: p.Day, DayRequests: 3}, 12, 3},
		{"this cycle, an earlier day", Usage{CycleStart: p.CycleStart, Requests: 12, Day: p.Day.AddDate(0, 0, -1), DayRequests: 3}, 12, 0},
		{"an earlier cycle", Usage{CycleStart: brtDate(2026, 9, 1), Requests: 12, Day: brtDate(2026, 9, 30), DayRequests: 3}, 0, 0},
		{"never used", Usage{}, 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.usage.At(p)
			if got.Requests != tt.requests || got.DayRequests != tt.today || !got.CycleStart.Equal(p.CycleStart) || !got.Day.Equal(p.Day) {
				t.Fatalf("At() = %+v, want %d this cycle and %d today", got, tt.requests, tt.today)
			}
		})
	}
}

func TestPlatformQueryNormalize(t *testing.T) {
	long := ""
	for range PlatformSearchMax + 20 {
		long += "a"
	}
	tests := []struct {
		name string
		in   PlatformQuery
		want PlatformQuery
	}{
		{"defaults", PlatformQuery{}, PlatformQuery{Page: 1, PageSize: PlatformDefaultPageSize}},
		{"a negative page starts at one", PlatformQuery{Page: -3, PageSize: 10}, PlatformQuery{Page: 1, PageSize: 10}},
		{"an oversized page is capped", PlatformQuery{Page: 2, PageSize: 5000}, PlatformQuery{Page: 2, PageSize: PlatformMaxPageSize}},
		{"the search is trimmed", PlatformQuery{Page: 1, PageSize: 10, Search: "  escola  "}, PlatformQuery{Page: 1, PageSize: 10, Search: "escola"}},
		{"a long search is cut", PlatformQuery{Page: 1, PageSize: 10, Search: long}, PlatformQuery{Page: 1, PageSize: 10, Search: long[:PlatformSearchMax]}},
		{"a huge page is capped", PlatformQuery{Page: math.MaxInt, PageSize: PlatformMaxPageSize}, PlatformQuery{Page: PlatformMaxPage, PageSize: PlatformMaxPageSize}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.in.Normalize(); got != tt.want {
				t.Fatalf("Normalize() = %+v, want %+v", got, tt.want)
			}
		})
	}
	if offset := (PlatformQuery{Page: 3, PageSize: 20}).Normalize().Offset(); offset != 40 {
		t.Fatalf("Offset() = %d, want 40", offset)
	}
	if offset := (PlatformQuery{Page: math.MaxInt, PageSize: math.MaxInt}).Normalize().Offset(); offset < 0 || offset > math.MaxInt32 {
		t.Fatalf("Offset() of the largest page = %d, want a positive offset Postgres accepts", offset)
	}
}

func TestPlatformPageTotalPages(t *testing.T) {
	tests := []struct {
		total int64
		size  int
		want  int
	}{{0, 20, 0}, {1, 20, 1}, {20, 20, 1}, {21, 20, 2}, {5, 0, 0}}
	for _, tt := range tests {
		if got := (PlatformPage{TotalItems: tt.total, PageSize: tt.size}).TotalPages(); got != tt.want {
			t.Fatalf("TotalPages(%d, %d) = %d, want %d", tt.total, tt.size, got, tt.want)
		}
	}
}

func TestOnlyAnIdentifiedPlatformAdminReadsEveryWorkspace(t *testing.T) {
	tests := []struct {
		name   string
		editor Editor
		want   bool
	}{
		{"a platform admin", Editor{UserID: "u-1", PlatformAdmin: true}, true},
		{"a workspace owner", Editor{UserID: "u-1", OwnerID: "u-1"}, false},
		{"an anonymous platform flag", Editor{UserID: " ", PlatformAdmin: true}, false},
		{"nobody", Editor{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.editor.CanReadPlatformUsage(); got != tt.want {
				t.Fatalf("CanReadPlatformUsage() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCoverageShares(t *testing.T) {
	tests := []struct {
		name    string
		summary leadmap.Summary
		address float64
		onMap   float64
	}{
		{"no leads", leadmap.Summary{}, 0, 0},
		{"half with an address, a quarter on the map", leadmap.Summary{Total: 8, WithoutAddress: 4, OnMap: 2, Approximate: 2}, 0.5, 0.25},
		{"everyone placed", leadmap.Summary{Total: 3, OnMap: 3}, 1, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := Coverage{Summary: tt.summary}
			if c.AddressShare() != tt.address || c.MapShare() != tt.onMap {
				t.Fatalf("shares = %v, %v, want %v, %v", c.AddressShare(), c.MapShare(), tt.address, tt.onMap)
			}
		})
	}
}
