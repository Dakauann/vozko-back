package shared

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestParseDate(t *testing.T) {
	cases := []struct {
		raw  string
		want Date
		err  error
	}{
		{"1990-04-21", Date{Year: 1990, Month: time.April, Day: 21}, nil},
		{" 2000-02-29 ", Date{Year: 2000, Month: time.February, Day: 29}, nil},
		{"2001-02-29", Date{}, ErrDateInvalid},
		{"21/04/1990", Date{}, ErrDateInvalid},
		{"", Date{}, ErrDateInvalid},
	}
	for _, tc := range cases {
		got, err := ParseDate(tc.raw)
		if !errors.Is(err, tc.err) || got != tc.want {
			t.Errorf("ParseDate(%q) = %v, %v; want %v, %v", tc.raw, got, err, tc.want, tc.err)
		}
	}
}

func TestDateYearsAt(t *testing.T) {
	birth := Date{Year: 1990, Month: time.April, Day: 21}
	leapBirth := Date{Year: 2000, Month: time.February, Day: 29}
	cases := []struct {
		name string
		date Date
		now  time.Time
		want int
	}{
		{"the day before the birthday", birth, time.Date(2026, time.April, 20, 23, 0, 0, 0, time.UTC), 35},
		{"on the birthday", birth, time.Date(2026, time.April, 21, 0, 0, 0, 0, time.UTC), 36},
		{"after the birthday", birth, time.Date(2026, time.December, 1, 0, 0, 0, 0, time.UTC), 36},
		{"a leap day birthday in a common year counts from March", leapBirth, time.Date(2025, time.February, 28, 0, 0, 0, 0, time.UTC), 24},
		{"a leap day birthday on March first", leapBirth, time.Date(2025, time.March, 1, 0, 0, 0, 0, time.UTC), 25},
		{"born today", Date{Year: 2026, Month: time.October, Day: 8}, time.Date(2026, time.October, 8, 12, 0, 0, 0, time.UTC), 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.date.YearsAt(tc.now); got != tc.want {
				t.Fatalf("YearsAt = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestDateAfter(t *testing.T) {
	d := Date{Year: 2026, Month: time.October, Day: 8}
	if d.After(time.Date(2026, time.October, 8, 23, 59, 0, 0, time.UTC)) {
		t.Fatal("a date is not after the same calendar day")
	}
	if !d.After(time.Date(2026, time.October, 7, 23, 59, 0, 0, time.UTC)) {
		t.Fatal("a date is after the previous calendar day")
	}
}

func TestDateJSONRoundTrip(t *testing.T) {
	d := Date{Year: 1990, Month: time.April, Day: 21}
	raw, err := json.Marshal(d)
	if err != nil || string(raw) != `"1990-04-21"` {
		t.Fatalf("Marshal = %s, %v", raw, err)
	}
	var back Date
	if err := json.Unmarshal(raw, &back); err != nil || back != d {
		t.Fatalf("Unmarshal = %v, %v", back, err)
	}
	if err := json.Unmarshal([]byte(`"1990-13-01"`), &back); !errors.Is(err, ErrDateInvalid) {
		t.Fatalf("an impossible date must be refused, got %v", err)
	}
}

func TestDateTimeIsMidnightUTC(t *testing.T) {
	d := Date{Year: 1990, Month: time.April, Day: 21}
	if got := d.Time(); !got.Equal(time.Date(1990, time.April, 21, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("Time = %v", got)
	}
	if DateOf(time.Date(1990, time.April, 21, 22, 0, 0, 0, time.UTC)) != d {
		t.Fatal("DateOf must keep the calendar day")
	}
}
