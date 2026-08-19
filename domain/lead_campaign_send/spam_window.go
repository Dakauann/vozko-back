package lead_campaign_send

import "time"

// WithinSpamWindow reports whether this lead was already reached from this
// number recently enough that reaching them again is spam.
//
// Extracted from the campaign consumer so cold outbound from the CRM honours the
// SAME cooldown as a bulk campaign. Left inlined, the dialog would have been a
// documented way to message someone the campaign pipeline had just refused to
// message — the workspace's own setting, bypassed by choosing a different button.
//
// A zero or negative window means the workspace has turned the protection off,
// and a lead never reached before has nothing to compare against; both are "not
// spam" rather than errors.
func WithinSpamWindow(lastSent *time.Time, protectionDays int, now time.Time) bool {
	if protectionDays <= 0 || lastSent == nil {
		return false
	}
	cutoff := now.UTC().AddDate(0, 0, -protectionDays)
	return lastSent.After(cutoff)
}
