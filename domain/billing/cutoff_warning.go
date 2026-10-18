package billing

import "time"

func InCutoffWarning(now, dueDate, cutoff time.Time) bool {
	loc := cutoff.Location()
	due := dueDate.In(loc)
	pastDue := time.Date(due.Year(), due.Month(), due.Day()+1, 0, 0, 0, 0, loc)
	return !now.Before(pastDue) && !now.After(cutoff)
}
