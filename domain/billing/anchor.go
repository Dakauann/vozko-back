package billing

import "time"

// Default billing knobs. Operational values are loaded from environment config; these document
// the defaults and serve as fallbacks. See UNIFIED_BILLING_IMPLEMENTATION_PLAN.md section 6.
const (
	DefaultEmitDay                  = 18
	DefaultDueDay                   = 23
	DefaultCancelDeadlineDay        = 27
	DefaultPlanFirstAnchorFloorDays = 10
)

// daysInMonth returns the number of days in the month of the given year/month. Day 0 of the
// following month is the last day of this month, and time.Date normalizes the December rollover.
func daysInMonth(year int, month time.Month, loc *time.Location) int {
	return time.Date(year, month+1, 0, 0, 0, 0, 0, loc).Day()
}

// addMonth advances a (year, month) pair by one calendar month, rolling the year at December.
func addMonth(year int, month time.Month) (int, time.Month) {
	if month == time.December {
		return year + 1, time.January
	}
	return year, month + 1
}

// anchorDate returns midnight on the anchor day for the given year/month, clamping the day to
// the last day of the month when dueDay exceeds the month length (e.g. 31 in February).
func anchorDate(year int, month time.Month, dueDay int, loc *time.Location) time.Time {
	day := dueDay
	if max := daysInMonth(year, month, loc); day > max {
		day = max
	}
	return time.Date(year, month, day, 0, 0, 0, 0, loc)
}

// NextAnchor returns the first anchor instant at or after `from` for a workspace whose anchor
// day is `dueDay`. The anchor is midnight in from's location, clamped to the last day of the
// month when dueDay does not exist that month. The boundary is midnight-precise: an instant
// later than midnight on the anchor day rolls to the next month's anchor.
func NextAnchor(from time.Time, dueDay int) time.Time {
	loc := from.Location()
	cand := anchorDate(from.Year(), from.Month(), dueDay, loc)
	if cand.Before(from) {
		y, m := addMonth(from.Year(), from.Month())
		cand = anchorDate(y, m, dueDay, loc)
	}
	return cand
}

// PlanFirstAnchor returns the first recurring plan charge date after `purchase`: the first
// anchor day strictly after purchase that is at least `floorDays` away. Because the saldo rolls
// over, a short first cycle is financially neutral; the floor only avoids a trivially short first
// cycle for perception. See plan section 4.
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

// CancelCutoff returns the latest instant (end of the cutoff day) by which an unpaid channel must
// have a cancellation_request registered, in localTZ, for the given month. 360dialog stops
// next-month billing for any cancellation registered during the current month, so the true
// deadline is "before month end"; the day-of-month cutoff (default 27) is comfortable slack
// against payment-confirmation delay, not a race against the vendor's day-1 instant. The cutoff
// day is clamped to the last day of the month for safety. See plan section 6.
func CancelCutoff(month time.Time, localTZ *time.Location, cutoffDay int) time.Time {
	y, mo := month.Year(), month.Month()
	day := cutoffDay
	if max := daysInMonth(y, mo, localTZ); day > max {
		day = max
	}
	return time.Date(y, mo, day, 23, 59, 59, 0, localTZ)
}
