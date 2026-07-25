package billing

import "time"

// saoPaulo is the billing timezone for anchor and sweep boundaries (BILLING_LOCAL_TZ). Brazil has
// observed no DST since 2019, so a fixed UTC-3 zone is a correct fallback when the host lacks the
// tz database. Anchor and cutoff day math must be computed in this location, never in UTC, so a
// signup near local midnight lands on the right calendar day.
var saoPaulo = resolveLocation(time.LoadLocation, "America/Sao_Paulo", time.FixedZone("America/Sao_Paulo", -3*60*60))

// resolveLocation returns the named location from load, or fallback when the host lacks the tz
// database. Injecting load keeps the fallback branch testable.
func resolveLocation(load func(string) (*time.Location, error), name string, fallback *time.Location) *time.Location {
	if loc, err := load(name); err == nil {
		return loc
	}
	return fallback
}

// LocationBRT returns the billing timezone (America/Sao_Paulo, UTC-3).
func LocationBRT() *time.Location { return saoPaulo }
