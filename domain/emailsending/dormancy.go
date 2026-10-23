package emailsending

import "time"

const DormancyAfter = 90 * 24 * time.Hour

func Dormant(lastActiveAt, now time.Time, hasLiveCampaign bool) bool {
	if hasLiveCampaign || lastActiveAt.IsZero() {
		return false
	}
	return !now.Before(lastActiveAt.Add(DormancyAfter))
}

func (a SenderAccount) LastActiveAt(lastSentAt *time.Time) time.Time {
	latest := a.CreatedAt
	if a.UpdatedAt.After(latest) {
		latest = a.UpdatedAt
	}
	if lastSentAt != nil && lastSentAt.After(latest) {
		latest = *lastSentAt
	}
	return latest
}

func (d SendingDomain) HasProviderIdentity() bool {
	if d.Retired {
		return false
	}
	switch d.Status {
	case DomainVerifying, DomainVerified, DomainFailed:
		return true
	}
	return false
}
