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

func ReferencePointOf(coverage Coverage, idx ReferenceIndex, query address.Postal, at time.Time) (Fix, error) {
	placed := query
	if placed.CityCode == "" {
		placed.CityCode = idx.CityCodeOf(query)
	}
	if !coverage.Covers(placed) {
		return Fix{}, ErrReferenceNotLoaded
	}
	fix, ok := Choose(nil, idx.Candidates(query, at))
	if !ok {
		return Fix{}, ErrReferencePointNotFound
	}
	return fix, nil
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
