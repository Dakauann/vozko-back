package emailhygiene

import (
	"strings"
	"time"

	es "vozko/domain/emailsending"
)

const (
	SunsetMonths        = 6
	SunsetMinCampaigns  = 5
	SoftStreakLimit     = 5
	SoftStreakSpan      = 14 * 24 * time.Hour
	machineOpenAgent    = "Mozilla/5.0"
	ActivityRetention   = 730 * 24 * time.Hour
	DomainCheckIdleDays = 180
)

type Activity struct {
	Known                 bool
	FirstDeliveredAt      *time.Time
	LastDeliveredAt       *time.Time
	LastEngagedAt         *time.Time
	SunsetSince           *time.Time
	CampaignsSinceEngaged int
	SoftStreak            int
	SoftStreakStartedAt   *time.Time
}

func (a Activity) EverDelivered() bool {
	return a.Known && a.FirstDeliveredAt != nil
}

func (a Activity) Inactive(now time.Time) bool {
	if !a.Known || a.SunsetSince == nil || a.CampaignsSinceEngaged < SunsetMinCampaigns {
		return false
	}
	return a.SunsetSince.Before(now.AddDate(0, -SunsetMonths, 0))
}

func Engages(ev es.DeliveryEvent) bool {
	if ev.IsBot {
		return false
	}
	switch ev.Kind {
	case es.EventClick:
		return true
	case es.EventOpen:
		return !IsMachineOpen(ev.UserAgent)
	}
	return false
}

func IsMachineOpen(userAgent string) bool {
	return strings.TrimSpace(userAgent) == machineOpenAgent
}

func CountsTowardSoftStreak(b *es.Bounce) bool {
	if b == nil || b.Kind != es.BounceSoft {
		return false
	}
	switch b.SubType {
	case "", "General", "MailboxFull", "Undetermined":
		return true
	}
	return false
}

func SoftStreakSuppresses(streak int, startedAt, lastAt time.Time) bool {
	return streak >= SoftStreakLimit && lastAt.Sub(startedAt) >= SoftStreakSpan
}
