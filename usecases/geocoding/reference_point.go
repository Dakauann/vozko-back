package geocoding_usecase

import (
	"context"
	"errors"
	"fmt"
	"time"

	"vozko/domain/address"
	"vozko/domain/geo"
)

var errReferencePointsIncomplete = errors.New("reference points: the reference data is missing")

type ReferencePoints struct {
	reference geo.Reference
	now       func() time.Time
}

func NewReferencePoints(reference geo.Reference, now func() time.Time) (*ReferencePoints, error) {
	if reference == nil {
		return nil, errReferencePointsIncomplete
	}
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &ReferencePoints{reference: reference, now: now}, nil
}

func (s *ReferencePoints) Locate(ctx context.Context, raw address.Postal) (geo.Fix, error) {
	query, err := geo.ReferenceQueryOf(raw)
	if err != nil {
		return geo.Fix{}, err
	}
	coverage, err := s.reference.Coverage(ctx)
	if err != nil {
		return geo.Fix{}, fmt.Errorf("%w: coverage: %w", geo.ErrReferenceUnavailable, err)
	}
	index, err := s.reference.Lookup(ctx, []address.Postal{query})
	if err != nil {
		return geo.Fix{}, fmt.Errorf("%w: lookup: %w", geo.ErrReferenceUnavailable, err)
	}
	return geo.ReferencePointOf(coverage, index, query, s.now())
}
