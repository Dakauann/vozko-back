package geo

import (
	"errors"
	"fmt"
	"time"

	"vozko/domain/address"
)

var (
	ErrReferenceQueryInvalid  = errors.New("geo: a reference point needs a CEP or a city with its state")
	ErrReferenceNotLoaded     = errors.New("geo: the reference data does not cover this place")
	ErrReferencePointNotFound = errors.New("geo: the reference data has no point for this place")
	ErrReferenceUnavailable   = errors.New("geo: the reference data could not be read")
)

func ReferenceQueryOf(raw address.Postal) (address.Postal, error) {
	if err := raw.Validate(); err != nil {
		return address.Postal{}, fmt.Errorf("%w: %w", ErrReferenceQueryInvalid, err)
	}
	return raw.Normalize(), nil
}

type ReferenceSpot struct {
	Fix       Fix
	City      string
	State     string
	WholeCity bool
}

func ReferencePointOf(coverage Coverage, idx ReferenceIndex, query address.Postal, at time.Time) (ReferenceSpot, error) {
	placed := query
	if placed.CityCode == "" {
		placed.CityCode = idx.CityCodeOf(query)
	}
	if !coverage.Covers(placed) {
		return ReferenceSpot{}, ErrReferenceNotLoaded
	}
	fix, ok := Choose(nil, idx.Candidates(query, at))
	if !ok {
		return ReferenceSpot{}, ErrReferencePointNotFound
	}
	spot := ReferenceSpot{Fix: fix, WholeCity: fix.Precision == PrecisionCity}
	city, known := idx.Cities[placed.CityCode]
	if !known {
		return spot, nil
	}
	spot.City, spot.State = city.Name, city.State
	if spot.WholeCity && city.Point.Validate() == nil {
		spot.Fix.Point = city.Point
	}
	return spot, nil
}

func ReferenceErrorCode(err error) string {
	switch {
	case errors.Is(err, ErrReferenceQueryInvalid):
		return "reference_query_invalid"
	case errors.Is(err, ErrReferenceNotLoaded):
		return string(ReasonReferenceNotLoaded)
	case errors.Is(err, ErrReferencePointNotFound):
		return "reference_point_not_found"
	case errors.Is(err, ErrReferenceUnavailable):
		return string(ReasonReferenceDown)
	}
	return ""
}
