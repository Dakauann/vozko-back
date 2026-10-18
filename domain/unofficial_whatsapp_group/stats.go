package unofficial_whatsapp_group

import (
	"errors"
	"time"
)

var ErrPeriodInvalid = errors.New("period must be month, 7d, 30d or 90d")

type Period string

const (
	PeriodMonth Period = "month"
	Period7d    Period = "7d"
	Period30d   Period = "30d"
	Period90d   Period = "90d"
)

const (
	DashboardWindowDays = 7
	GrowthWindowDays    = 30
	OverviewWindowDays  = 30
)

type Window struct {
	From time.Time
	To   time.Time
}

func ResolveWindow(p Period, now time.Time, loc *time.Location) (Window, error) {
	local := now.In(loc)
	today := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
	switch p {
	case PeriodMonth:
		return Window{From: time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, loc), To: now}, nil
	case Period7d:
		return Window{From: today.AddDate(0, 0, -6), To: now}, nil
	case Period30d:
		return Window{From: today.AddDate(0, 0, -29), To: now}, nil
	case Period90d:
		return Window{From: today.AddDate(0, 0, -89), To: now}, nil
	}
	return Window{}, ErrPeriodInvalid
}

func LastDays(days int, now time.Time, loc *time.Location) Window {
	local := now.In(loc)
	today := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
	return Window{From: today.AddDate(0, 0, -(days - 1)), To: now}
}

func (w Window) Days(loc *time.Location) []time.Time {
	var days []time.Time
	from := w.From.In(loc)
	day := time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, loc)
	for !day.After(w.To) {
		days = append(days, day)
		day = day.AddDate(0, 0, 1)
	}
	return days
}

func DayKey(t time.Time, loc *time.Location) string {
	return t.In(loc).Format("2006-01-02")
}

const MinCrossGroups = 2

func CrossGroupMinimum(requested int) int {
	if requested < MinCrossGroups {
		return MinCrossGroups
	}
	return requested
}

func GrowthRate(net, base int64) *float64 {
	if base <= 0 {
		return nil
	}
	rate := float64(net) / float64(base) * 100
	return &rate
}

func ChurnRate(leaves, base int64) *float64 {
	if base <= 0 {
		return nil
	}
	rate := float64(leaves) / float64(base) * 100
	return &rate
}

type DailyCount struct {
	Messages     int64
	ActiveGroups int64
	Joins        int64
	Leaves       int64
}

type DailyPoint struct {
	Day          time.Time `json:"day"`
	Messages     int64     `json:"messages"`
	ActiveGroups int64     `json:"activeGroups"`
	Joins        int64     `json:"joins"`
	Leaves       int64     `json:"leaves"`
	Net          int64     `json:"net"`
}

func FillDaily(w Window, loc *time.Location, byDay map[string]DailyCount) []DailyPoint {
	days := w.Days(loc)
	out := make([]DailyPoint, 0, len(days))
	for _, day := range days {
		c := byDay[DayKey(day, loc)]
		out = append(out, DailyPoint{
			Day: day, Messages: c.Messages, ActiveGroups: c.ActiveGroups,
			Joins: c.Joins, Leaves: c.Leaves, Net: c.Joins - c.Leaves,
		})
	}
	return out
}

func FillResponseTimes(w Window, loc *time.Location, byDay map[string]ResponseTimePoint) []ResponseTimePoint {
	days := w.Days(loc)
	out := make([]ResponseTimePoint, 0, len(days))
	for _, day := range days {
		p := byDay[DayKey(day, loc)]
		p.Day = day
		out = append(out, p)
	}
	return out
}
