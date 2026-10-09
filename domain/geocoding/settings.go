package geocoding

import (
	"errors"
	"strings"
	"time"

	"vozko/domain/billing"
	"vozko/domain/shared"
	"vozko/domain/workspace"
)

const (
	DefaultMonthlyCeiling int64 = 5000
	MaxMonthlyCeiling     int64 = 1_000_000
	dailyShareMultiple          = 2
	cycleDay                    = 1
)

var (
	ErrSettingsForbidden     = errors.New("geocoding: only the workspace owner or a platform admin can choose the external provider")
	ErrCeilingForbidden      = errors.New("geocoding: only a platform admin can change the monthly ceiling")
	ErrChangeEmpty           = errors.New("geocoding: send provider or monthlyCeiling")
	ErrProviderUnknown       = errors.New("geocoding: unknown provider")
	ErrProviderNotConfigured = errors.New("geocoding: this server has no access to that provider")
	ErrCeilingInvalid        = errors.New("geocoding: the monthly ceiling must be between 0 and the platform maximum")
	ErrProviderOff           = errors.New("geocoding: the workspace has not enabled an external provider")
	ErrNoRoom                = errors.New("geocoding: the workspace has no external requests left")
	ErrSettingsUnreadable    = errors.New("geocoding: the workspace geocoding settings could not be read")
)

type Provider string

const ProviderOpenCage Provider = "opencage"

func (p Provider) Known() bool { return p == ProviderOpenCage }

type Settings struct {
	WorkspaceID       string
	Provider          Provider
	ProviderChangedBy string
	ProviderChangedAt *time.Time
	MonthlyCeiling    *int64
	CeilingChangedBy  string
	CeilingChangedAt  *time.Time
}

func (s Settings) Enabled() bool { return s.Provider.Known() }

func (s Settings) Ceiling() int64 {
	if !s.Enabled() {
		return 0
	}
	if s.MonthlyCeiling != nil {
		return max(*s.MonthlyCeiling, 0)
	}
	return DefaultMonthlyCeiling
}

func (s Settings) Quota() shared.MonthlyQuota {
	return shared.MonthlyQuota{Limit: s.Ceiling(), CycleDay: cycleDay, Location: billing.LocationBRT()}
}

type Slot struct {
	CycleStart    time.Time
	NextCycle     time.Time
	Day           time.Time
	NextDay       time.Time
	MonthlyLimit  int64
	DailyLimit    int64
	Provider      Provider
	StoredCeiling *int64
}

func (s Settings) SlotAt(now time.Time) (Slot, error) {
	if !s.Enabled() {
		return Slot{}, ErrProviderOff
	}
	quota := s.Quota()
	if quota.CheckRoom(0) != nil {
		return Slot{}, ErrNoRoom
	}
	period := PeriodAt(now)
	day, start, next := period.Day, period.CycleStart, period.NextCycle
	days := int64(next.Sub(start).Round(24*time.Hour) / (24 * time.Hour))
	daily := (quota.Limit*dailyShareMultiple + days - 1) / max(days, 1)
	var stored *int64
	if s.MonthlyCeiling != nil {
		value := *s.MonthlyCeiling
		stored = &value
	}
	return Slot{
		CycleStart: start, NextCycle: next, Day: day, NextDay: period.NextDay,
		MonthlyLimit: quota.Limit, DailyLimit: min(max(daily, 1), quota.Limit),
		Provider: s.Provider, StoredCeiling: stored,
	}, nil
}

type Usage struct {
	CycleStart  time.Time
	Requests    int64
	Day         time.Time
	DayRequests int64
}

func (u Usage) In(slot Slot) Usage {
	return u.At(Period{CycleStart: slot.CycleStart, NextCycle: slot.NextCycle, Day: slot.Day, NextDay: slot.NextDay})
}

func (u Usage) At(p Period) Usage {
	current := Usage{CycleStart: p.CycleStart, Day: p.Day}
	if u.CycleStart.Equal(p.CycleStart) {
		current.Requests = u.Requests
	}
	if u.Day.Equal(p.Day) {
		current.DayRequests = u.DayRequests
	}
	return current
}

type Exhaustion string

const (
	ExhaustedNone    Exhaustion = ""
	ExhaustedMonthly Exhaustion = "monthly"
	ExhaustedDaily   Exhaustion = "daily"
)

func (slot Slot) Refusal(usage Usage) (Exhaustion, time.Time) {
	current := usage.In(slot)
	if current.Requests >= slot.MonthlyLimit {
		return ExhaustedMonthly, slot.NextCycle
	}
	if current.DayRequests >= slot.DailyLimit {
		return ExhaustedDaily, slot.NextDay
	}
	return ExhaustedNone, time.Time{}
}

func (slot Slot) Refused(usage Usage) (ProviderStep, Exhaustion) {
	exhaustion, until := slot.Refusal(usage)
	if exhaustion == ExhaustedNone {
		return Deferred(), ExhaustedNone
	}
	return QuotaReached(until), exhaustion
}

type Change struct {
	Provider       *Provider
	MonthlyCeiling *int64
}

type Editor struct {
	UserID        string
	OwnerID       string
	PlatformAdmin bool
}

func (e Editor) identified() bool { return strings.TrimSpace(e.UserID) != "" }

func (e Editor) CanChangeProvider() bool {
	return workspace.OwnerOrPlatformAdmin(e.UserID, e.OwnerID, e.PlatformAdmin)
}

func (e Editor) CanChangeCeiling() bool { return e.identified() && e.PlatformAdmin }

func (e Editor) CanReadPlatformUsage() bool { return e.identified() && e.PlatformAdmin }

func (s Settings) Apply(change Change, editor Editor, configured func(Provider) bool, now time.Time) (Settings, error) {
	if change.Provider == nil && change.MonthlyCeiling == nil {
		return s, ErrChangeEmpty
	}
	next, stamp := s, now.UTC()
	actor := strings.TrimSpace(editor.UserID)
	if change.Provider != nil {
		if !editor.CanChangeProvider() {
			return s, ErrSettingsForbidden
		}
		wanted := Provider(strings.ToLower(strings.TrimSpace(string(*change.Provider))))
		if wanted != "" && !wanted.Known() {
			return s, ErrProviderUnknown
		}
		if wanted != "" && (configured == nil || !configured(wanted)) {
			return s, ErrProviderNotConfigured
		}
		if wanted != s.Provider {
			next.Provider, next.ProviderChangedBy, next.ProviderChangedAt = wanted, actor, &stamp
		}
	}
	if change.MonthlyCeiling != nil {
		if !editor.CanChangeCeiling() {
			return s, ErrCeilingForbidden
		}
		value := *change.MonthlyCeiling
		if value < 0 || value > MaxMonthlyCeiling {
			return s, ErrCeilingInvalid
		}
		if s.MonthlyCeiling == nil || *s.MonthlyCeiling != value {
			next.MonthlyCeiling, next.CeilingChangedBy, next.CeilingChangedAt = &value, actor, &stamp
		}
	}
	return next, nil
}
