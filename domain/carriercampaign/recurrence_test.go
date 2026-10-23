package carriercampaign

import (
	"errors"
	"testing"
	"time"

	"vozko/domain/shared"
)

const saoPaulo = "America/Sao_Paulo"

func mustLocation(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

func until(t *testing.T, raw string) *shared.Date {
	t.Helper()
	d, err := shared.ParseDate(raw)
	if err != nil {
		t.Fatal(err)
	}
	return &d
}

func TestRecurrenceValidate(t *testing.T) {
	cases := []struct {
		name string
		r    Recurrence
		err  error
	}{
		{"daily", Recurrence{Frequency: FrequencyDaily, Time: "09:00", Timezone: saoPaulo}, nil},
		{"weekly", Recurrence{Frequency: FrequencyWeekly, Time: "18:30", Weekdays: []time.Weekday{time.Monday, time.Friday}, Timezone: saoPaulo}, nil},
		{"monthly", Recurrence{Frequency: FrequencyMonthly, Time: "00:00", DayOfMonth: 31, Timezone: saoPaulo}, nil},
		{"unknown frequency", Recurrence{Frequency: "yearly", Time: "09:00", Timezone: saoPaulo}, ErrRecurrenceFrequency},
		{"empty frequency", Recurrence{Time: "09:00", Timezone: saoPaulo}, ErrRecurrenceFrequency},
		{"bad time", Recurrence{Frequency: FrequencyDaily, Time: "9:00", Timezone: saoPaulo}, ErrRecurrenceTime},
		{"hour out of range", Recurrence{Frequency: FrequencyDaily, Time: "24:00", Timezone: saoPaulo}, ErrRecurrenceTime},
		{"minute out of range", Recurrence{Frequency: FrequencyDaily, Time: "10:60", Timezone: saoPaulo}, ErrRecurrenceTime},
		{"missing timezone", Recurrence{Frequency: FrequencyDaily, Time: "09:00"}, ErrRecurrenceTimezone},
		{"unknown timezone", Recurrence{Frequency: FrequencyDaily, Time: "09:00", Timezone: "Mars/Olympus"}, ErrRecurrenceTimezone},
		{"weekly without days", Recurrence{Frequency: FrequencyWeekly, Time: "09:00", Timezone: saoPaulo}, ErrRecurrenceWeekdays},
		{"weekly repeated day", Recurrence{Frequency: FrequencyWeekly, Time: "09:00", Weekdays: []time.Weekday{time.Monday, time.Monday}, Timezone: saoPaulo}, ErrRecurrenceWeekdays},
		{"weekly unknown day", Recurrence{Frequency: FrequencyWeekly, Time: "09:00", Weekdays: []time.Weekday{7}, Timezone: saoPaulo}, ErrRecurrenceWeekdays},
		{"daily with days", Recurrence{Frequency: FrequencyDaily, Time: "09:00", Weekdays: []time.Weekday{time.Monday}, Timezone: saoPaulo}, ErrRecurrenceWeekdays},
		{"monthly without day", Recurrence{Frequency: FrequencyMonthly, Time: "09:00", Timezone: saoPaulo}, ErrRecurrenceDayOfMonth},
		{"monthly day 32", Recurrence{Frequency: FrequencyMonthly, Time: "09:00", DayOfMonth: 32, Timezone: saoPaulo}, ErrRecurrenceDayOfMonth},
		{"weekly with day of month", Recurrence{Frequency: FrequencyWeekly, Time: "09:00", Weekdays: []time.Weekday{time.Monday}, DayOfMonth: 3, Timezone: saoPaulo}, ErrRecurrenceDayOfMonth},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.r.Validate(); !errors.Is(err, tc.err) {
				t.Fatalf("err = %v, want %v", err, tc.err)
			}
		})
	}
}

func TestRecurrenceNext(t *testing.T) {
	sp := mustLocation(t, saoPaulo)
	ny := mustLocation(t, "America/New_York")
	at := func(loc *time.Location, y int, m time.Month, d, hh, mm int) time.Time {
		return time.Date(y, m, d, hh, mm, 0, 0, loc)
	}
	daily := Recurrence{Frequency: FrequencyDaily, Time: "09:00", Timezone: saoPaulo}
	weekly := Recurrence{Frequency: FrequencyWeekly, Time: "10:30", Weekdays: []time.Weekday{time.Friday, time.Monday, time.Wednesday}, Timezone: saoPaulo}
	monthly31 := Recurrence{Frequency: FrequencyMonthly, Time: "08:00", DayOfMonth: 31, Timezone: saoPaulo}
	monthly29 := Recurrence{Frequency: FrequencyMonthly, Time: "08:00", DayOfMonth: 29, Timezone: saoPaulo}

	cases := []struct {
		name  string
		r     Recurrence
		after time.Time
		want  time.Time
		ok    bool
	}{
		{"daily later today", daily, at(sp, 2026, 10, 21, 7, 0), at(sp, 2026, 10, 21, 9, 0), true},
		{"daily strictly after the occurrence itself", daily, at(sp, 2026, 10, 21, 9, 0), at(sp, 2026, 10, 22, 9, 0), true},
		{"daily tomorrow", daily, at(sp, 2026, 10, 21, 9, 1), at(sp, 2026, 10, 22, 9, 0), true},
		{"daily across a utc date change", daily, time.Date(2026, 10, 22, 1, 0, 0, 0, time.UTC), at(sp, 2026, 10, 22, 9, 0), true},
		{"sao paulo keeps -03 across november", daily, at(sp, 2026, 10, 31, 10, 0), time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC), true},
		{"sao paulo keeps -03 in summer", daily, at(sp, 2027, 1, 15, 10, 0), time.Date(2027, 1, 16, 12, 0, 0, 0, time.UTC), true},
		{"wall clock kept where dst ends", Recurrence{Frequency: FrequencyDaily, Time: "09:00", Timezone: "America/New_York"}, at(ny, 2026, 10, 31, 9, 30), time.Date(2026, 11, 1, 14, 0, 0, 0, time.UTC), true},
		{"weekly same day before time", weekly, at(sp, 2026, 10, 21, 10, 0), at(sp, 2026, 10, 21, 10, 30), true},
		{"weekly wednesday to friday", weekly, at(sp, 2026, 10, 21, 11, 0), at(sp, 2026, 10, 23, 10, 30), true},
		{"weekly friday to monday", weekly, at(sp, 2026, 10, 23, 10, 30), at(sp, 2026, 10, 26, 10, 30), true},
		{"weekly sunday to monday", weekly, at(sp, 2026, 10, 25, 23, 59), at(sp, 2026, 10, 26, 10, 30), true},
		{"monthly clamps to 30 days", monthly31, at(sp, 2026, 11, 1, 0, 0), at(sp, 2026, 11, 30, 8, 0), true},
		{"monthly clamps to february", monthly31, at(sp, 2027, 1, 31, 9, 0), at(sp, 2027, 2, 28, 8, 0), true},
		{"monthly leap february", monthly31, at(sp, 2028, 2, 1, 0, 0), at(sp, 2028, 2, 29, 8, 0), true},
		{"monthly day 29 in short february", monthly29, at(sp, 2027, 2, 1, 0, 0), at(sp, 2027, 2, 28, 8, 0), true},
		{"monthly back to 31 after clamp", monthly31, at(sp, 2027, 2, 28, 8, 0), at(sp, 2027, 3, 31, 8, 0), true},
		{"monthly same month later", Recurrence{Frequency: FrequencyMonthly, Time: "08:00", DayOfMonth: 5, Timezone: saoPaulo}, at(sp, 2026, 10, 5, 7, 59), at(sp, 2026, 10, 5, 8, 0), true},
		{"monthly next year", Recurrence{Frequency: FrequencyMonthly, Time: "08:00", DayOfMonth: 5, Timezone: saoPaulo}, at(sp, 2026, 12, 5, 8, 0), at(sp, 2027, 1, 5, 8, 0), true},
		{"until includes its own day", Recurrence{Frequency: FrequencyDaily, Time: "09:00", Timezone: saoPaulo, Until: until(t, "2026-10-22")}, at(sp, 2026, 10, 21, 10, 0), at(sp, 2026, 10, 22, 9, 0), true},
		{"past until", Recurrence{Frequency: FrequencyDaily, Time: "09:00", Timezone: saoPaulo, Until: until(t, "2026-10-22")}, at(sp, 2026, 10, 22, 9, 0), time.Time{}, false},
		{"until judged on the local date", Recurrence{Frequency: FrequencyDaily, Time: "22:00", Timezone: saoPaulo, Until: until(t, "2026-10-21")}, at(sp, 2026, 10, 21, 8, 0), at(sp, 2026, 10, 21, 22, 0), true},
		{"invalid recurrence never fires", Recurrence{Frequency: FrequencyWeekly, Time: "09:00", Timezone: saoPaulo}, at(sp, 2026, 10, 21, 8, 0), time.Time{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := tc.r.Next(tc.after)
			if ok != tc.ok {
				t.Fatalf("ok = %v, want %v", ok, tc.ok)
			}
			if !got.Equal(tc.want) {
				t.Fatalf("got %s, want %s", got, tc.want)
			}
		})
	}
}

func TestRecurrenceNextWalksAWeek(t *testing.T) {
	sp := mustLocation(t, saoPaulo)
	r := Recurrence{Frequency: FrequencyWeekly, Time: "10:30", Weekdays: []time.Weekday{time.Monday, time.Wednesday, time.Friday}, Timezone: saoPaulo}
	cursor := time.Date(2026, 10, 18, 0, 0, 0, 0, sp)
	var got []time.Weekday
	for range 6 {
		next, ok := r.Next(cursor)
		if !ok {
			t.Fatal("expected an occurrence")
		}
		got = append(got, next.In(sp).Weekday())
		cursor = next
	}
	want := []time.Weekday{time.Monday, time.Wednesday, time.Friday, time.Monday, time.Wednesday, time.Friday}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("occurrence %d on %s, want %s", i, got[i], want[i])
		}
	}
}
