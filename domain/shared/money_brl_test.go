package shared

import "testing"

func TestBRLFromMicrosRoundsToCents(t *testing.T) {
	cases := []struct {
		micros int64
		rate   float64
		want   float64
	}{
		{0, 5.5, 0},
		{1_000_000, 5.5, 5.5},
		{1166, 5.5, 0.01},
		{583, 5.5, 0},
		{699_600, 6, 4.2},
		{-1_000_000, 5, -5},
		{1_000_000, 0, 0},
	}
	for _, tc := range cases {
		if got := BRLFromMicros(tc.micros, tc.rate); got != tc.want {
			t.Errorf("BRLFromMicros(%d, %v) = %v, want %v", tc.micros, tc.rate, got, tc.want)
		}
	}
}

func TestEstimateBRLFromMicrosKeepsSixDecimals(t *testing.T) {
	cases := []struct {
		micros int64
		rate   float64
		want   float64
	}{
		{583, 5.5, 0.003207},
		{583, 6, 0.003498},
		{13_333, 6, 0.079998},
		{699_600, 6, 4.1976},
		{100, 5.5, 0.00055},
		{583_000, 6, 3.498},
		{-1_000_000, 5, -5},
		{1_000_000, 0, 0},
		{0, 6, 0},
	}
	for _, tc := range cases {
		if got := EstimateBRLFromMicros(tc.micros, tc.rate); got != tc.want {
			t.Errorf("EstimateBRLFromMicros(%d, %v) = %v, want %v", tc.micros, tc.rate, got, tc.want)
		}
	}
}
