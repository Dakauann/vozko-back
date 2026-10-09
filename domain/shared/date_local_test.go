package shared

import (
	"errors"
	"testing"
	"time"
)

func TestParseLocalDate(t *testing.T) {
	cases := []struct {
		raw  string
		want Date
		ok   bool
	}{
		{"2026-10-08", Date{2026, time.October, 8}, true},
		{" 08/10/2026 ", Date{2026, time.October, 8}, true},
		{"8/1/1990", Date{1990, time.January, 8}, true},
		{"08-10-2026", Date{2026, time.October, 8}, true},
		{"08.10.2026", Date{2026, time.October, 8}, true},
		{"2026-10-08T10:30:00-03:00", Date{2026, time.October, 8}, true},
		{"08/10/2026 14:30", Date{2026, time.October, 8}, true},
		{"31/02/2026", Date{}, false},
		{"10/08/26", Date{}, false},
		{"ontem", Date{}, false},
		{"", Date{}, false},
	}
	for _, tc := range cases {
		got, err := ParseLocalDate(tc.raw)
		if tc.ok {
			if err != nil || got != tc.want {
				t.Errorf("ParseLocalDate(%q) = %v, %v; want %v", tc.raw, got, err, tc.want)
			}
			continue
		}
		if !errors.Is(err, ErrDateInvalid) {
			t.Errorf("ParseLocalDate(%q) err = %v, want ErrDateInvalid", tc.raw, err)
		}
	}
}
