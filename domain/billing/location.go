package billing

import "time"

var saoPaulo = resolveLocation(time.LoadLocation, "America/Sao_Paulo", time.FixedZone("America/Sao_Paulo", -3*60*60))

func resolveLocation(load func(string) (*time.Location, error), name string, fallback *time.Location) *time.Location {
	if loc, err := load(name); err == nil {
		return loc
	}
	return fallback
}

func LocationBRT() *time.Location { return saoPaulo }
