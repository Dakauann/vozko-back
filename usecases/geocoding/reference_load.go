package geocoding_usecase

import (
	"context"
	"errors"
	"fmt"
	"time"

	"vozko/domain/georef"
)

const referenceSeed = 2022

var (
	ErrReferenceEmpty      = errors.New("geocoding reference: the file has no address with a usable position")
	errReferenceIncomplete = errors.New("geocoding reference: a required dependency is missing")
)

type ReferenceSource interface {
	Stream(ctx context.Context, uf georef.UFFile, each func(georef.Record)) (int64, error)
}

type AddressWaker interface {
	WakeUnavailable(ctx context.Context, now time.Time) (int64, error)
}

type ReferenceLoaderDeps struct {
	Source         ReferenceSource
	Store          georef.Store
	Waker          AddressWaker
	Municipalities map[string]georef.Municipality
	Limits         georef.Limits
	Now            func() time.Time
}

type ReferenceLoad struct {
	georef.Load
	Stats         georef.Stats
	MissingCities []string
	Woken         int64
}

type ReferenceLoader struct {
	deps ReferenceLoaderDeps
}

func NewReferenceLoader(deps ReferenceLoaderDeps) (*ReferenceLoader, error) {
	if deps.Source == nil || deps.Store == nil || deps.Waker == nil {
		return nil, errReferenceIncomplete
	}
	if deps.Now == nil {
		deps.Now = func() time.Time { return time.Now().UTC() }
	}
	return &ReferenceLoader{deps: deps}, nil
}

func (l *ReferenceLoader) Load(ctx context.Context, uf georef.UFFile, dry bool) (ReferenceLoad, error) {
	builder := georef.NewBuilder(l.deps.Limits, referenceSeed)
	rows, err := l.deps.Source.Stream(ctx, uf, func(r georef.Record) { builder.Add(r) })
	if err != nil {
		return ReferenceLoad{}, fmt.Errorf("geocoding reference %s: %w", uf.State, err)
	}
	ceps := builder.CEPPoints()
	if len(ceps) == 0 {
		return ReferenceLoad{}, fmt.Errorf("%w: %s", ErrReferenceEmpty, uf.State)
	}
	districts := builder.DistrictPoints()
	streets := builder.Streets()
	cities, missing := georef.Cities(builder.CityPoints(), l.deps.Municipalities)
	builtAt := l.deps.Now()
	load := ReferenceLoad{
		Load: georef.Load{
			State: uf.State, Rows: rows, Measurement: georef.MeasureSpread(ceps),
			Districts: len(districts), Cities: len(cities), Streets: len(streets), Source: georef.Attribution, BuiltAt: builtAt,
		},
		Stats:         builder.Stats(),
		MissingCities: missing,
	}
	if dry {
		return load, nil
	}
	if err := l.deps.Store.UpsertCEPs(ctx, ceps, builtAt); err != nil {
		return load, fmt.Errorf("geocoding reference %s: CEP points: %w", uf.State, err)
	}
	if err := l.deps.Store.UpsertDistricts(ctx, districts, builtAt); err != nil {
		return load, fmt.Errorf("geocoding reference %s: bairro points: %w", uf.State, err)
	}
	if err := l.deps.Store.UpsertCities(ctx, cities, builtAt); err != nil {
		return load, fmt.Errorf("geocoding reference %s: cities: %w", uf.State, err)
	}
	if err := l.deps.Store.ReplaceStreets(ctx, uf.State, streets, builtAt); err != nil {
		return load, fmt.Errorf("geocoding reference %s: streets: %w", uf.State, err)
	}
	if err := l.deps.Store.RecordLoad(ctx, load.Load); err != nil {
		return load, fmt.Errorf("geocoding reference %s: record the load: %w", uf.State, err)
	}
	woken, err := l.deps.Waker.WakeUnavailable(ctx, l.deps.Now())
	load.Woken = woken
	if err != nil {
		return load, fmt.Errorf("geocoding reference %s: wake the addresses waiting for it: %w", uf.State, err)
	}
	return load, nil
}
