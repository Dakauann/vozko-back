package shared

import (
	"errors"
	"testing"
	"time"
)

func TestADayWindowCoversWholeLocalDaysUpToItsLimit(t *testing.T) {
	saoPaulo := time.FixedZone("BRT", -3*3600)
	cases := []struct {
		name     string
		from, to string
		loc      *time.Location
		max      int
		want     error
		days     int
	}{
		{"one day", "2026-10-01", "2026-10-01", saoPaulo, 7, nil, 1},
		{"exactly the limit", "2026-10-01", "2026-10-07", saoPaulo, 7, nil, 7},
		{"one day past the limit", "2026-10-01", "2026-10-08", saoPaulo, 7, ErrDayWindowTooLong, 0},
		{"reversed", "2026-10-02", "2026-10-01", saoPaulo, 7, ErrDayWindowInvalid, 0},
		{"not a day", "01/10/2026", "2026-10-01", saoPaulo, 7, ErrDayWindowInvalid, 0},
		{"no zone", "2026-10-01", "2026-10-01", nil, 7, ErrDayWindowInvalid, 0},
		{"no limit", "2026-10-01", "2026-10-01", saoPaulo, 0, ErrDayWindowInvalid, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w, err := NewDayWindowWithin(tc.from, tc.to, tc.loc, tc.max)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if err != nil {
				return
			}
			if len(w.Days()) != tc.days || w.FirstDay() != tc.from || w.LastDay() != tc.to {
				t.Fatalf("window %s..%s has %d days", w.FirstDay(), w.LastDay(), len(w.Days()))
			}
			if !w.From.Equal(time.Date(2026, 10, 1, 0, 0, 0, 0, saoPaulo)) {
				t.Fatalf("from = %v, want local midnight", w.From)
			}
		})
	}
}
