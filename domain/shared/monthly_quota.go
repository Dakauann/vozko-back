package shared

import (
	"errors"
	"time"
)

var ErrMonthlyQuotaReached = errors.New("monthly quota reached")

type MonthlyQuota struct {
	Limit    int64
	CycleDay int
	Location *time.Location
}

func MonthlyCycleStart(now time.Time, cycleDay int, loc *time.Location) time.Time {
	if loc == nil {
		loc = time.UTC
	}
	day := max(cycleDay, 1)
	local := now.In(loc)
	start := monthlyCycleDate(local.Year(), local.Month(), day, loc)
	if local.Before(start) {
		return monthlyCycleDate(local.Year(), local.Month()-1, day, loc)
	}
	return start
}

func monthlyCycleDate(year int, month time.Month, cycleDay int, loc *time.Location) time.Time {
	first := time.Date(year, month, 1, 0, 0, 0, 0, loc)
	lastDay := first.AddDate(0, 1, -1).Day()
	return first.AddDate(0, 0, min(cycleDay, lastDay)-1)
}

func (q MonthlyQuota) CycleStart(now time.Time) time.Time {
	return MonthlyCycleStart(now, q.CycleDay, q.Location)
}

func (q MonthlyQuota) NextCycleStart(now time.Time) time.Time {
	start := q.CycleStart(now)
	return monthlyCycleDate(start.Year(), start.Month()+1, max(q.CycleDay, 1), start.Location())
}

func (q MonthlyQuota) CheckRoom(used int64) error {
	if q.Limit <= 0 || used >= q.Limit {
		return ErrMonthlyQuotaReached
	}
	return nil
}

func (q MonthlyQuota) Remaining(used int64) int64 {
	if q.Limit <= 0 || used >= q.Limit {
		return 0
	}
	return q.Limit - used
}
