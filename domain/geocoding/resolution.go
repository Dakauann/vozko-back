package geocoding

import (
	"strings"
	"time"

	"vozko/domain/geo"
	"vozko/domain/lead"
)

const (
	firstBackoff  = time.Minute
	backoffFactor = 4
	MaxBackoff    = 6 * time.Hour
)

type SkipReason string

const (
	SkipProviderOff   SkipReason = "provider_off"
	SkipPreciseEnough SkipReason = "precise_enough"
	SkipNoStreet      SkipReason = "no_street"
	SkipConfirmed     SkipReason = "confirmed"
	SkipReferenceDown SkipReason = "reference_down"
	SkipNoCeiling     SkipReason = "no_ceiling"
	SkipStale         SkipReason = "stale_fingerprint"
)

type stepKind int

const (
	stepSkipped stepKind = iota
	stepAnswered
	stepQuota
	stepDeferred
)

type ProviderStep struct {
	kind    stepKind
	Skip    SkipReason
	Outcome geo.Outcome
	Until   time.Time
}

func Skipped(reason SkipReason) ProviderStep { return ProviderStep{kind: stepSkipped, Skip: reason} }

func Answered(outcome geo.Outcome) ProviderStep {
	return ProviderStep{kind: stepAnswered, Outcome: outcome}
}

func QuotaReached(until time.Time) ProviderStep { return ProviderStep{kind: stepQuota, Until: until} }

func Deferred() ProviderStep { return ProviderStep{kind: stepDeferred} }

func (s ProviderStep) Called() bool { return s.kind == stepAnswered }

func WantsProvider(a lead.Address, candidates []geo.Fix) SkipReason {
	if a.Fix != nil && a.Fix.Source.Confirmed() {
		return SkipConfirmed
	}
	if best, _ := geo.Choose(a.Fix, candidates); best.Precision.PinsAHouse() {
		return SkipPreciseEnough
	}
	if strings.TrimSpace(a.Postal.Street) == "" {
		return SkipNoStreet
	}
	return ""
}

func StaleClaim(a lead.Address, fingerprint string) bool {
	return fingerprint != a.Fingerprint()
}

type Attempt struct {
	Address       lead.Address
	Fingerprint   string
	Attempts      int
	ReferenceDown geo.UnavailableReason
	Candidates    []geo.Fix
	Provider      ProviderStep
	Now           time.Time
}

type Resolution struct {
	Address lead.Address
	NextAt  *time.Time
	Changed bool
}

func (r Resolution) Outcome() string {
	if r.Address.Fix != nil {
		return string(r.Address.Fix.Precision)
	}
	return string(r.Address.GeoStatus)
}

func Resolve(a Attempt) (Resolution, error) {
	before := a.Address
	addr := a.Address
	if StaleClaim(addr, a.Fingerprint) {
		addr.GeoStatus = lead.GeoUnavailable
		return Resolution{Address: addr, NextAt: timeAt(a.Now.Add(Backoff(a.Attempts))), Changed: changed(before, addr)}, nil
	}
	candidates := append([]geo.Fix(nil), a.Candidates...)
	if a.Provider.kind == stepAnswered && a.Provider.Outcome.Kind == geo.OutcomeLocated {
		candidates = append(candidates, a.Provider.Outcome.Fix)
	}
	if best, ok := geo.Choose(nil, candidates); ok && a.ReferenceDown == "" {
		if _, err := addr.ApplyGeocode(best, a.Fingerprint); err != nil {
			return Resolution{}, err
		}
	}
	status, next := settle(addr, a)
	addr.GeoStatus = status
	return Resolution{Address: addr, NextAt: next, Changed: changed(before, addr)}, nil
}

func settle(addr lead.Address, a Attempt) (lead.GeoStatus, *time.Time) {
	if a.ReferenceDown != "" {
		return lead.GeoUnavailable, timeAt(a.Now.Add(Backoff(a.Attempts)))
	}
	switch a.Provider.kind {
	case stepQuota:
		return lead.GeoQuotaExceeded, timeAt(a.Provider.Until)
	case stepDeferred:
		return lead.GeoPending, timeAt(a.Now)
	case stepAnswered:
		if RefusalOf(a.Provider.Outcome) == RefusalQuery {
			return lead.GeoRefused, nil
		}
		if a.Provider.Outcome.Kind == geo.OutcomeUnavailable {
			return lead.GeoUnavailable, timeAt(a.Now.Add(max(Backoff(a.Attempts), a.Provider.Outcome.RetryAfter)))
		}
	}
	if addr.Fix != nil {
		return lead.StatusOfFix(*addr.Fix), nil
	}
	if a.Provider.kind == stepAnswered && a.Provider.Outcome.Kind == geo.OutcomeAmbiguous {
		return lead.GeoAmbiguous, nil
	}
	return lead.GeoNotFound, nil
}

func changed(before, after lead.Address) bool {
	return before.GeoStatus != after.GeoStatus || !geo.SameFix(before.Fix, after.Fix)
}

func timeAt(t time.Time) *time.Time { return &t }

func Backoff(attempts int) time.Duration {
	wait := firstBackoff
	for i := 1; i < attempts; i++ {
		wait *= backoffFactor
		if wait >= MaxBackoff {
			return MaxBackoff
		}
	}
	return wait
}
