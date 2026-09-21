package billing

import "time"

const (
	DefaultEmitDay                  = 18
	DefaultDueDay                   = 23
	DefaultCancelDeadlineDay        = 27
	DefaultPlanFirstAnchorFloorDays = 10
)

func daysInMonth(year int, month time.Month, loc *time.Location) int {
	return time.Date(year, month+1, 0, 0, 0, 0, 0, loc).Day()
}

func addMonth(year int, month time.Month) (int, time.Month) {
	if month == time.December {
		return year + 1, time.January
	}
	return year, month + 1
}

func anchorDate(year int, month time.Month, dueDay int, loc *time.Location) time.Time {
	day := dueDay
	if max := daysInMonth(year, month, loc); day > max {
		day = max
	}
	return time.Date(year, month, day, 0, 0, 0, 0, loc)
}

func NextAnchor(from time.Time, dueDay int) time.Time {
	loc := from.Location()
	cand := anchorDate(from.Year(), from.Month(), dueDay, loc)
	if cand.Before(from) {
		y, m := addMonth(from.Year(), from.Month())
		cand = anchorDate(y, m, dueDay, loc)
	}
	return cand
}

func PlanFirstAnchor(purchase time.Time, dueDay, floorDays int) time.Time {
	loc := purchase.Location()
	floor := time.Duration(floorDays) * 24 * time.Hour
	y, m := purchase.Year(), purchase.Month()
	for {
		a := anchorDate(y, m, dueDay, loc)
		if a.After(purchase) && a.Sub(purchase) >= floor {
			return a
		}
		y, m = addMonth(y, m)
	}
}

func CancelCutoff(month time.Time, localTZ *time.Location, cutoffDay int) time.Time {
	y, mo := month.Year(), month.Month()
	day := cutoffDay
	if max := daysInMonth(y, mo, localTZ); day > max {
		day = max
	}
	return time.Date(y, mo, day, 23, 59, 59, 0, localTZ)
}
