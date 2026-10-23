package calendar

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

const wholeDayLayout = "2006-01-02"

var (
	ErrDateTimeFormat  = errors.New("use RFC3339 ou formatos como 2026-03-31 14:00")
	ErrRangeNotForward = errors.New("a data de fim precisa ser depois da data de início")
)

type Clock struct {
	Hour   int
	Minute int
}

var (
	DefaultWorkStart = Clock{Hour: 8}
	DefaultWorkEnd   = Clock{Hour: 18}
)

func ParseClock(raw string, fallback Clock) Clock {
	parsed, err := time.Parse("15:04", strings.TrimSpace(raw))
	if err != nil {
		return fallback
	}
	return Clock{Hour: parsed.Hour(), Minute: parsed.Minute()}
}

func (c Clock) On(day time.Time, loc *time.Location) time.Time {
	return time.Date(day.Year(), day.Month(), day.Day(), c.Hour, c.Minute, 0, 0, loc)
}

func ParseDateTime(raw string, location *time.Location) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05", "2006-01-02T15:04", "2006-01-02 15:04:05", "2006-01-02 15:04"} {
		parsed, err := time.ParseInLocation(layout, raw, location)
		if err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, ErrDateTimeFormat
}

func AvailabilityRange(from, to string, workStart, workEnd Clock, loc *time.Location) (time.Time, time.Time, error) {
	start, err := availabilityBound(from, workStart, loc)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("data/hora início inválida: %w", err)
	}
	end, err := availabilityEnd(start, to, workEnd, loc)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("data/hora fim inválida: %w", err)
	}
	if !end.After(start) {
		return time.Time{}, time.Time{}, ErrRangeNotForward
	}
	return start, end, nil
}

func availabilityEnd(start time.Time, to string, workEnd Clock, loc *time.Location) (time.Time, error) {
	if strings.TrimSpace(to) != "" {
		return availabilityBound(to, workEnd, loc)
	}
	day := start.In(loc)
	if closing := workEnd.On(day, loc); closing.After(start) {
		return closing, nil
	}
	return time.Date(day.Year(), day.Month(), day.Day()+1, 0, 0, 0, 0, loc), nil
}

func availabilityBound(raw string, wholeDayClock Clock, loc *time.Location) (time.Time, error) {
	if day, err := time.ParseInLocation(wholeDayLayout, strings.TrimSpace(raw), loc); err == nil {
		return wholeDayClock.On(day, loc), nil
	}
	return ParseDateTime(raw, loc)
}
