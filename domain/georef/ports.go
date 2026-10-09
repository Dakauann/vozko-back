package georef

import (
	"context"
	"time"

	"vozko/domain/address"
	"vozko/domain/geo"
)

type Load struct {
	State       string
	Rows        int64
	Measurement SpreadMeasurement
	Districts   int
	Cities      int
	Streets     int
	Source      string
	BuiltAt     time.Time
}

type Store interface {
	UpsertCEPs(ctx context.Context, points []CEPPoint, builtAt time.Time) error
	UpsertDistricts(ctx context.Context, points []DistrictPoint, builtAt time.Time) error
	UpsertCities(ctx context.Context, cities []City, builtAt time.Time) error
	ReplaceStreets(ctx context.Context, state string, streets []Street, builtAt time.Time) error
	RecordLoad(ctx context.Context, load Load) error
}

type LocatedAddresses interface {
	LocatedAfter(ctx context.Context, afterID string, limit int) ([]LocatedAddress, error)
}

type CityCodes interface {
	CityCodes(ctx context.Context, postals []address.Postal) (geo.ReferenceIndex, error)
}

type DistrictRefinements interface {
	RefineDistricts(ctx context.Context, points []DistrictPoint, builtAt time.Time) error
	DropLeadDistrictsBefore(ctx context.Context, builtAt time.Time) (int64, error)
}

type RefineRuns interface {
	LastRefine(ctx context.Context) (time.Time, error)
	MarkRefine(ctx context.Context, at time.Time) error
}

type PlaceIndex interface {
	Loads(ctx context.Context) ([]LoadStamp, error)
	Cities(ctx context.Context, q PlaceQuery, limit int) ([]Place, error)
	Districts(ctx context.Context, q PlaceQuery, limit int) ([]Place, error)
	Streets(ctx context.Context, q PlaceQuery, limit int) ([]Place, error)
	CEPs(ctx context.Context, q PlaceQuery, limit int) ([]Place, error)
}

type CEPStreets interface {
	CEPStreets(ctx context.Context, zip string) ([]CEPStreetRow, error)
}
