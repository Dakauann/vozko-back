package unofficial_whatsapp

import "time"

const (
	DefaultDailySendCap = 1000

	WarmupDays = 21

	MinWarmupDailyCap = 50
)

func (i *Instance) EffectiveDailyCap(now time.Time) int {
	cap := i.DailySendCap
	if cap <= 0 {
		cap = DefaultDailySendCap
	}
	if i.WarmupStartedAt == nil {
		return cap
	}

	elapsed := now.UTC().Sub(i.WarmupStartedAt.UTC())
	day := int(elapsed.Hours()/24) + 1
	if day >= WarmupDays {
		return cap
	}
	if day < 1 {
		day = 1
	}

	ramped := cap * day / WarmupDays
	if ramped < MinWarmupDailyCap {
		if cap < MinWarmupDailyCap {
			return cap
		}
		return MinWarmupDailyCap
	}
	return ramped
}

func (i *Instance) InWarmup(now time.Time) bool {
	if i.WarmupStartedAt == nil {
		return false
	}
	return now.UTC().Sub(i.WarmupStartedAt.UTC()) < WarmupDays*24*time.Hour
}
