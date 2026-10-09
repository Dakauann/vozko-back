package geocoding_usecase

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"vozko/domain/address"
	"vozko/domain/georef"
)

var errDistrictRefineIncomplete = errors.New("geocoding district refine: a required dependency is missing")

type DistrictRefineDeps struct {
	Addresses georef.LocatedAddresses
	Cities    georef.CityCodes
	Districts georef.DistrictRefinements
	Runs      georef.RefineRuns
	Now       func() time.Time
	BatchSize int
	Limits    georef.Limits
}

type DistrictRefine struct {
	deps DistrictRefineDeps
}

func NewDistrictRefine(deps DistrictRefineDeps) (*DistrictRefine, error) {
	if deps.Addresses == nil || deps.Cities == nil || deps.Districts == nil || deps.Runs == nil {
		return nil, errDistrictRefineIncomplete
	}
	if deps.Now == nil {
		deps.Now = func() time.Time { return time.Now().UTC() }
	}
	if deps.BatchSize <= 0 {
		deps.BatchSize = georef.RefineBatch
	}
	return &DistrictRefine{deps: deps}, nil
}

func (r *DistrictRefine) Run(ctx context.Context) error {
	now := r.deps.Now()
	last, err := r.deps.Runs.LastRefine(ctx)
	if err != nil {
		return fmt.Errorf("geocoding district refine: last run: %w", err)
	}
	if !georef.RefineDueAt(now, last) {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, georef.RefineBudget)
	defer cancel()
	refiner := georef.NewDistrictRefiner(r.deps.Limits)
	if err := r.read(ctx, refiner); err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("geocoding district refine: unfinished after %d addresses within the %s budget: %w", refiner.Stats().Addresses, georef.RefineBudget, err)
		}
		return err
	}
	points := refiner.Points()
	if err := r.deps.Districts.RefineDistricts(ctx, points, now); err != nil {
		return fmt.Errorf("geocoding district refine: write: %w", err)
	}
	dropped, err := r.deps.Districts.DropLeadDistrictsBefore(ctx, now)
	if err != nil {
		return fmt.Errorf("geocoding district refine: drop stale: %w", err)
	}
	if err := r.deps.Runs.MarkRefine(ctx, now); err != nil {
		return fmt.Errorf("geocoding district refine: record run: %w", err)
	}
	stats := refiner.Stats()
	log.Printf("[geocoding] district refine: addresses=%d used=%d skipped=%d bairros=%d dropped=%d",
		stats.Addresses, stats.Used, stats.Skipped, len(points), dropped)
	return nil
}

func (r *DistrictRefine) read(ctx context.Context, refiner *georef.DistrictRefiner) error {
	after := ""
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		page, err := r.deps.Addresses.LocatedAfter(ctx, after, r.deps.BatchSize)
		if err != nil {
			return fmt.Errorf("geocoding district refine: addresses: %w", err)
		}
		if len(page) == 0 {
			return nil
		}
		postals := make([]address.Postal, len(page))
		for i, a := range page {
			postals[i] = a.Postal
		}
		index, err := r.deps.Cities.CityCodes(ctx, postals)
		if err != nil {
			return fmt.Errorf("geocoding district refine: city codes: %w", err)
		}
		for _, a := range page {
			refiner.Add(index.CityCodeOf(a.Postal), a)
		}
		if len(page) < r.deps.BatchSize {
			return nil
		}
		after = page[len(page)-1].ID
	}
}
