package advertising

import (
	"fmt"
	"time"
)

const (
	DayLayout    = "2006-01-02"
	MaxRangeDays = 731
)

type DateRange struct {
	Since time.Time
	Until time.Time
}

func ParseDay(raw string) (time.Time, error) {
	day, err := time.ParseInLocation(DayLayout, raw, time.UTC)
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: day %q", ErrInvalidRange, raw)
	}
	return day, nil
}

func NewDateRange(since, until string) (DateRange, error) {
	from, err := ParseDay(since)
	if err != nil {
		return DateRange{}, err
	}
	to, err := ParseDay(until)
	if err != nil {
		return DateRange{}, err
	}
	r := DateRange{Since: from, Until: to}
	if err := r.Validate(); err != nil {
		return DateRange{}, err
	}
	return r, nil
}

func (r DateRange) Validate() error {
	if r.Since.IsZero() || r.Until.IsZero() || r.Until.Before(r.Since) {
		return fmt.Errorf("%w: %s to %s", ErrInvalidRange, r.Since.Format(DayLayout), r.Until.Format(DayLayout))
	}
	if r.Days() > MaxRangeDays {
		return fmt.Errorf("%w: longer than %d days", ErrInvalidRange, MaxRangeDays)
	}
	return nil
}

func (r DateRange) Days() int {
	return int(r.Until.Sub(r.Since).Hours()/24) + 1
}

func (r DateRange) Contains(day time.Time) bool {
	d := CivilDay(day, time.UTC)
	return !d.Before(r.Since) && !d.After(r.Until)
}

func CivilDay(t time.Time, loc *time.Location) time.Time {
	local := t.In(loc)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.UTC)
}

func LastDays(n int, now time.Time, loc *time.Location) DateRange {
	until := CivilDay(now, loc)
	return DateRange{Since: until.AddDate(0, 0, -(n - 1)), Until: until}
}

func (r DateRange) Bounds(loc *time.Location) (time.Time, time.Time) {
	start := time.Date(r.Since.Year(), r.Since.Month(), r.Since.Day(), 0, 0, 0, 0, loc)
	end := time.Date(r.Until.Year(), r.Until.Month(), r.Until.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, 1)
	return start, end
}
