package crmfilter

import (
	"errors"
	"fmt"
	"time"
)

const (
	BirthdayToday     = "today"
	BirthdayThisWeek  = "this_week"
	BirthdayThisMonth = "this_month"
)

const lastDayOfAnyMonth = 31

var ErrBirthdayClockMissing = errors.New("crmfilter: a birthday window needs the workspace day")

type MonthDay struct {
	Month int
	Day   int
}

type MonthDayRange struct {
	From MonthDay
	To   MonthDay
}

func birthdayValue(v string) error {
	switch v {
	case BirthdayToday, BirthdayThisWeek, BirthdayThisMonth:
		return nil
	}
	return ErrInvalidValue
}

func BirthdayRanges(windows []string, today time.Time) ([]MonthDayRange, error) {
	if today.IsZero() {
		return nil, ErrBirthdayClockMissing
	}
	var out []MonthDayRange
	for _, window := range nonEmpty(windows) {
		ranges, err := birthdayWindow(window, today)
		if err != nil {
			return nil, err
		}
		out = append(out, ranges...)
	}
	return out, nil
}

func birthdayWindow(window string, today time.Time) ([]MonthDayRange, error) {
	switch window {
	case BirthdayToday:
		return []MonthDayRange{dayRange(today)}, nil
	case BirthdayThisWeek:
		start := today.AddDate(0, 0, -int(today.Weekday()))
		return spanRanges(start, start.AddDate(0, 0, 6)), nil
	case BirthdayThisMonth:
		month := int(today.Month())
		return []MonthDayRange{{From: MonthDay{month, 1}, To: MonthDay{month, lastDayOfAnyMonth}}}, nil
	}
	return nil, fmt.Errorf("%w: unknown birthday window %q", ErrInvalidValue, window)
}

func dayRange(today time.Time) MonthDayRange {
	at := monthDayOf(today)
	until := at
	if at.Month == int(time.February) && at.Day == 28 && !leapYear(today.Year()) {
		until.Day = 29
	}
	return MonthDayRange{From: at, To: until}
}

func spanRanges(from, to time.Time) []MonthDayRange {
	if from.Year() == to.Year() {
		return []MonthDayRange{{From: monthDayOf(from), To: monthDayOf(to)}}
	}
	return []MonthDayRange{
		{From: monthDayOf(from), To: MonthDay{12, 31}},
		{From: MonthDay{1, 1}, To: monthDayOf(to)},
	}
}

func monthDayOf(t time.Time) MonthDay {
	return MonthDay{Month: int(t.Month()), Day: t.Day()}
}

func leapYear(year int) bool {
	return year%4 == 0 && (year%100 != 0 || year%400 == 0)
}
