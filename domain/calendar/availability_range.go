package calendar

import (
	"fmt"
	"strings"
	"time"
)

func ParseDateTime(raw string, location *time.Location) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05", "2006-01-02T15:04", "2006-01-02 15:04:05", "2006-01-02 15:04"} {
		parsed, err := time.ParseInLocation(layout, raw, location)
		if err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, fmt.Errorf("use RFC3339 ou formatos como 2026-03-31 14:00")
}

func AvailabilityRange(from, to, workingStart, workingEnd string, loc *time.Location) (time.Time, time.Time, error) {
	parse := func(raw, clock string) (time.Time, error) {
		if date, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(raw), loc); err == nil {
			parsedClock, err := time.Parse("15:04", clock)
			if err != nil {
				return time.Time{}, fmt.Errorf("horário de expediente inválido: %s", clock)
			}
			return time.Date(date.Year(), date.Month(), date.Day(), parsedClock.Hour(), parsedClock.Minute(), 0, 0, loc), nil
		}
		return ParseDateTime(raw, loc)
	}
	if workingStart == "" {
		workingStart = "08:00"
	}
	if workingEnd == "" {
		workingEnd = "18:00"
	}
	start, err := parse(from, workingStart)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("data/hora início inválida: %w", err)
	}
	if strings.TrimSpace(to) == "" {
		to = start.In(loc).Format("2006-01-02")
	}
	end, err := parse(to, workingEnd)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("data/hora fim inválida: %w", err)
	}
	if !end.After(start) {
		return time.Time{}, time.Time{}, fmt.Errorf("a data de fim precisa ser depois da data de início")
	}
	return start, end, nil
}
