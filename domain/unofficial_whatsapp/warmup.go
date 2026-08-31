package unofficial_whatsapp

import "time"

// Daily send budget and the warmup ramp.
//
// None of this is offered by the provider or by WhatsApp. It is ours, and it is
// the highest-value ban-avoidance control in the channel: a number that goes
// from zero to five thousand messages on its first day is the clearest
// automation signature WhatsApp has, and the cost of tripping it is the customer
// losing their WhatsApp — not a rejected API call.
//
// DailySendCap and WarmupStartedAt have existed as columns and as editable
// settings since the channel shipped. Nothing read them. This file is what makes
// them load-bearing.

const (
	// DefaultDailySendCap is what a number is allowed before anyone configures
	// it. Conservative on purpose: the failure mode of too low is a campaign
	// that takes an extra day, and of too high is a number that is gone.
	DefaultDailySendCap = 1000

	// WarmupDays is how long a new number ramps to its full cap.
	WarmupDays = 21

	// MinWarmupDailyCap is the floor on day one, so a brand-new number can still
	// do useful work while it warms up.
	MinWarmupDailyCap = 50
)

// EffectiveDailyCap is how many messages this number may send today.
//
// A zero DailySendCap means UNKNOWN, not unlimited, and falls back to the
// default. The safe reading matters for the same reason it does on
// Server.HasCapacity: guessing "unlimited" means blasting a number nobody has
// configured a limit for.
//
// A nil WarmupStartedAt means the number is not warming up — an established
// number connected to the platform, rather than a fresh one — and gets its full
// cap. Warmup is opt-in per number because we cannot tell the two apart.
func (i *Instance) EffectiveDailyCap(now time.Time) int {
	cap := i.DailySendCap
	if cap <= 0 {
		cap = DefaultDailySendCap
	}
	if i.WarmupStartedAt == nil {
		return cap
	}

	// Day 1 is the day warmup started, so a number that just started warming up
	// gets 1/21 of its cap rather than nothing.
	elapsed := now.UTC().Sub(i.WarmupStartedAt.UTC())
	day := int(elapsed.Hours()/24) + 1
	if day >= WarmupDays {
		return cap
	}
	if day < 1 {
		// A warmup start stamped in the future is a misconfiguration, not
		// permission to send at full rate.
		day = 1
	}

	ramped := cap * day / WarmupDays
	if ramped < MinWarmupDailyCap {
		// Never below the floor, and never above the configured cap either: a
		// cap set lower than the floor is a deliberate restriction and must win.
		if cap < MinWarmupDailyCap {
			return cap
		}
		return MinWarmupDailyCap
	}
	return ramped
}

// InWarmup reports whether this number is still ramping, so the UI can say so
// rather than showing a cap that looks arbitrarily low.
func (i *Instance) InWarmup(now time.Time) bool {
	if i.WarmupStartedAt == nil {
		return false
	}
	return now.UTC().Sub(i.WarmupStartedAt.UTC()) < WarmupDays*24*time.Hour
}
