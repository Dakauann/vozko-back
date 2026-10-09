package geocoding

import (
	"time"

	"vozko/domain/geo"
	"vozko/domain/lead"
)

const (
	firstBackoff  = time.Minute
	backoffFactor = 4
	MaxBackoff    = 6 * time.Hour
)

func StaleClaim(a lead.Address, fingerprint string) bool {
	return fingerprint != a.Fingerprint()
}

type Attempt struct {
	Address       lead.Address
	Fingerprint   string
	Attempts      int
	ReferenceDown geo.UnavailableReason
	Candidates    []geo.Fix
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
	if StaleClaim(addr, a.Fingerprint) || a.ReferenceDown != "" {
		addr.GeoStatus = lead.GeoUnavailable
		next := a.Now.Add(Backoff(a.Attempts))
		return Resolution{Address: addr, NextAt: &next, Changed: changed(before, addr)}, nil
	}
	if best, ok := geo.Choose(nil, a.Candidates); ok {
		if _, err := addr.ApplyGeocode(best, a.Fingerprint); err != nil {
			return Resolution{}, err
		}
	}
	addr.GeoStatus = lead.GeoNotFound
	if addr.Fix != nil {
		addr.GeoStatus = lead.StatusOfFix(*addr.Fix)
	}
	return Resolution{Address: addr, Changed: changed(before, addr)}, nil
}

func changed(before, after lead.Address) bool {
	return before.GeoStatus != after.GeoStatus || !geo.SameFix(before.Fix, after.Fix)
}

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
