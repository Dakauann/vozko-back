package billing

import (
	"testing"
	"time"
)

func TestBillableSeconds(t *testing.T) {
	answered := time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC)
	cases := []struct {
		name     string
		answered time.Time
		ended    time.Time
		want     int
	}{
		{"never answered is free", time.Time{}, answered.Add(40 * time.Second), 0},
		{"ended at the answer instant still counts", answered, answered, 1},
		{"sub-second answered call counts", answered, answered.Add(400 * time.Millisecond), 1},
		{"exact minute", answered, answered.Add(time.Minute), 60},
		{"a fraction over a minute rounds up", answered, answered.Add(time.Minute + 200*time.Millisecond), 61},
		{"clock skew before the answer counts as the minimum", answered, answered.Add(-time.Second), 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := BillableSeconds(tc.answered, tc.ended); got != tc.want {
				t.Fatalf("BillableSeconds() = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestBilledMinutesFollowTheWholeMinuteRule(t *testing.T) {
	answered := time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC)
	cases := []struct {
		talk time.Duration
		want int64
	}{
		{0, 1},
		{30 * time.Second, 1},
		{60 * time.Second, 1},
		{61 * time.Second, 2},
		{120 * time.Second, 2},
		{121 * time.Second, 3},
	}
	for _, tc := range cases {
		if got := BilledMinutes(BillableSeconds(answered, answered.Add(tc.talk))); got != tc.want {
			t.Errorf("talk %v billed %d minutes, want %d", tc.talk, got, tc.want)
		}
	}
	if got := BilledMinutes(0); got != 0 {
		t.Errorf("BilledMinutes(0) = %d, want 0 for an unanswered call", got)
	}
}

func TestMinuteWavesReserveBeforeEachMinuteAndStopAtCoverage(t *testing.T) {
	answered := time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC)
	waves := MinuteWaves{AnsweredAt: answered, Minute: time.Minute, Lead: 10 * time.Second}

	if got := waves.CoverageEnd(1); !got.Equal(answered.Add(time.Minute)) {
		t.Fatalf("CoverageEnd(1) = %v, want one minute after answer", got)
	}
	if got := waves.NextReservationAt(1); !got.Equal(answered.Add(50 * time.Second)) {
		t.Fatalf("NextReservationAt(1) = %v, want 10s before the second minute", got)
	}
	if got := waves.NextReservationAt(3); !got.Equal(answered.Add(170 * time.Second)) {
		t.Fatalf("NextReservationAt(3) = %v, want 10s before the fourth minute", got)
	}
	short := MinuteWaves{AnsweredAt: answered, Minute: 5 * time.Second, Lead: 10 * time.Second}
	if got := short.NextReservationAt(1); !got.Equal(answered) {
		t.Fatalf("a lead longer than the minute must clamp to the answer time, got %v", got)
	}
}
