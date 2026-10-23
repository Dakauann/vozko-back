package emailsending

import "time"

const (
	Uncapped              int64 = -1
	ProbationDailyCap     int64 = 500
	TrustedSharedDailyCap int64 = 5000
)

const TimeZoneName = "America/Sao_Paulo"

var brasilia = time.FixedZone("BRT", -3*60*60)

type DailyLimit string

const (
	DailyLimitCapped    DailyLimit = "capped"
	DailyLimitUnlimited DailyLimit = "unlimited"
	DailyLimitBlocked   DailyLimit = "blocked"
)

func DayKey(at time.Time) string {
	return at.In(brasilia).Format("2006-01-02")
}

func DayStart(at time.Time) time.Time {
	local := at.In(brasilia)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, brasilia).UTC()
}

func NextDayStart(at time.Time) time.Time {
	local := at.In(brasilia)
	next := time.Date(local.Year(), local.Month(), local.Day()+1, 0, 0, 0, 0, brasilia)
	return next.UTC()
}

func DailyCap(domain SendingDomain) int64 {
	switch domain.Kind {
	case DomainSharedProbation:
		return ProbationDailyCap
	case DomainSharedTrusted:
		return TrustedSharedDailyCap
	case DomainOwn:
		if domain.Sendable() {
			return Uncapped
		}
	}
	return 0
}

func DailyLimitOf(capValue int64) DailyLimit {
	switch {
	case capValue == Uncapped:
		return DailyLimitUnlimited
	case capValue > 0:
		return DailyLimitCapped
	}
	return DailyLimitBlocked
}
