package lead_campaign_send

import "time"

func WithinSpamWindow(lastSent *time.Time, protectionDays int, now time.Time) bool {
	if protectionDays <= 0 || lastSent == nil {
		return false
	}
	cutoff := now.UTC().AddDate(0, 0, -protectionDays)
	return lastSent.After(cutoff)
}
