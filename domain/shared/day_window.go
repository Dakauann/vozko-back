package shared

import (
	"errors"
	"time"
)

const dayWindowLayout = "2006-01-02"

var (
	ErrDayWindowInvalid = errors.New("day window is invalid")
	ErrDayWindowTooLong = errors.New("day window exceeds the maximum number of days")
)

type DayWindow struct {
	From     time.Time
	To       time.Time
	Location *time.Location
}

func NewDayWindowWithin(fromDay, toDay string, loc *time.Location, maxDays int) (DayWindow, error) {
	if loc == nil || maxDays <= 0 {
		return DayWindow{}, ErrDayWindowInvalid
	}
	from, err := time.ParseInLocation(dayWindowLayout, fromDay, loc)
	if err != nil {
		return DayWindow{}, ErrDayWindowInvalid
	}
	last, err := time.ParseInLocation(dayWindowLayout, toDay, loc)
	if err != nil || last.Before(from) {
		return DayWindow{}, ErrDayWindowInvalid
	}
	w := DayWindow{From: from, To: last.AddDate(0, 0, 1), Location: loc}
	if w.To.After(from.AddDate(0, 0, maxDays)) {
		return DayWindow{}, ErrDayWindowTooLong
	}
	return w, nil
}

func (w DayWindow) FirstDay() string {
	return w.From.Format(dayWindowLayout)
}

func (w DayWindow) LastDay() string {
	return w.To.AddDate(0, 0, -1).Format(dayWindowLayout)
}

func (w DayWindow) Fingerprint() string {
	return w.FirstDay() + ":" + w.LastDay() + ":" + w.Location.String()
}

func (w DayWindow) Days() []string {
	var days []string
	for d := w.From; d.Before(w.To); d = d.AddDate(0, 0, 1) {
		days = append(days, d.Format(dayWindowLayout))
	}
	return days
}
