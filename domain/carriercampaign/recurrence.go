package carriercampaign

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"vozko/domain/shared"
)

type Frequency string

const (
	FrequencyDaily   Frequency = "daily"
	FrequencyWeekly  Frequency = "weekly"
	FrequencyMonthly Frequency = "monthly"
)

const (
	recurrenceTimeLayout  = "15:04"
	recurrenceHorizonDays = 62
	maxDayOfMonth         = 31
)

var (
	ErrRecurrenceFrequency  = errors.New("carriercampaign: a recurrence repeats daily, weekly or monthly")
	ErrRecurrenceTime       = errors.New("carriercampaign: the recurrence time must be HH:MM")
	ErrRecurrenceTimezone   = errors.New("carriercampaign: the recurrence needs a known time zone")
	ErrRecurrenceWeekdays   = errors.New("carriercampaign: a weekly recurrence needs distinct weekdays, and only a weekly one takes them")
	ErrRecurrenceDayOfMonth = errors.New("carriercampaign: a monthly recurrence needs a day of month from 1 to 31, and only a monthly one takes it")
)

type Recurrence struct {
	Frequency  Frequency      `json:"frequency"`
	Time       string         `json:"time"`
	Weekdays   []time.Weekday `json:"weekdays,omitempty"`
	DayOfMonth int            `json:"day_of_month,omitempty"`
	Timezone   string         `json:"timezone"`
	Until      *shared.Date   `json:"until,omitempty"`
}

func (r Recurrence) Validate() error {
	switch r.Frequency {
	case FrequencyDaily, FrequencyWeekly, FrequencyMonthly:
	default:
		return fmt.Errorf("%w: %q", ErrRecurrenceFrequency, r.Frequency)
	}
	if _, err := r.clock(); err != nil {
		return err
	}
	if _, err := r.location(); err != nil {
		return err
	}
	if err := r.validateWeekdays(); err != nil {
		return err
	}
	return r.validateDayOfMonth()
}

func (r Recurrence) clock() (time.Time, error) {
	raw := strings.TrimSpace(r.Time)
	parsed, err := time.Parse(recurrenceTimeLayout, raw)
	if err != nil || len(raw) != len(recurrenceTimeLayout) {
		return time.Time{}, fmt.Errorf("%w: %q", ErrRecurrenceTime, r.Time)
	}
	return parsed, nil
}

func (r Recurrence) location() (*time.Location, error) {
	name := strings.TrimSpace(r.Timezone)
	if name == "" {
		return nil, ErrRecurrenceTimezone
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil, fmt.Errorf("%w: %q", ErrRecurrenceTimezone, name)
	}
	return loc, nil
}

func (r Recurrence) validateWeekdays() error {
	if r.Frequency != FrequencyWeekly {
		if len(r.Weekdays) > 0 {
			return ErrRecurrenceWeekdays
		}
		return nil
	}
	if len(r.Weekdays) == 0 {
		return ErrRecurrenceWeekdays
	}
	seen := map[time.Weekday]bool{}
	for _, day := range r.Weekdays {
		if day < time.Sunday || day > time.Saturday || seen[day] {
			return ErrRecurrenceWeekdays
		}
		seen[day] = true
	}
	return nil
}

func (r Recurrence) validateDayOfMonth() error {
	if r.Frequency != FrequencyMonthly {
		if r.DayOfMonth != 0 {
			return ErrRecurrenceDayOfMonth
		}
		return nil
	}
	if r.DayOfMonth < 1 || r.DayOfMonth > maxDayOfMonth {
		return ErrRecurrenceDayOfMonth
	}
	return nil
}

func (r Recurrence) Next(after time.Time) (time.Time, bool) {
	if r.Validate() != nil {
		return time.Time{}, false
	}
	loc, _ := r.location()
	clock, _ := r.clock()
	y, m, d := after.In(loc).Date()
	for offset := 0; offset <= recurrenceHorizonDays; offset++ {
		day := time.Date(y, m, d+offset, 12, 0, 0, 0, loc)
		if !r.fallsOn(day) {
			continue
		}
		dy, dm, dd := day.Date()
		candidate := time.Date(dy, dm, dd, clock.Hour(), clock.Minute(), 0, 0, loc)
		if !candidate.After(after) {
			continue
		}
		if r.Until != nil && shared.DateOf(candidate).Time().After(r.Until.Time()) {
			return time.Time{}, false
		}
		return candidate, true
	}
	return time.Time{}, false
}

func (r Recurrence) fallsOn(day time.Time) bool {
	switch r.Frequency {
	case FrequencyWeekly:
		for _, wd := range r.Weekdays {
			if wd == day.Weekday() {
				return true
			}
		}
		return false
	case FrequencyMonthly:
		return day.Day() == min(r.DayOfMonth, daysIn(day.Year(), day.Month(), day.Location()))
	}
	return true
}

func daysIn(year int, month time.Month, loc *time.Location) int {
	return time.Date(year, month+1, 0, 12, 0, 0, 0, loc).Day()
}
