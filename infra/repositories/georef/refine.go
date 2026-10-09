package georef_repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/lib/pq"
	"gorm.io/gorm"

	"vozko/domain/address"
	domainCache "vozko/domain/cache"
	"vozko/domain/geo"
	"vozko/domain/georef"
)

const (
	firstAddressID = "00000000-0000-0000-0000-000000000000"

	locatedAfterSQL = "SELECT a.id::text AS id, a.workspace_id::text AS workspace_id, a.zip_code, a.city_code, a.city, a.state, a.district, a.latitude, a.longitude, a.geo_precision, a.geo_source" +
		" FROM lead_addresses a" +
		" WHERE a.id > ?::uuid AND a.latitude IS NOT NULL AND a.longitude IS NOT NULL" +
		" AND a.geo_precision = ANY(?) AND a.geo_source = ANY(?)" +
		" AND a.district_key IS NOT NULL AND a.district_key <> ''" +
		" AND EXISTS (SELECT 1 FROM leads l WHERE l.id = a.lead_id AND l.deleted_at IS NULL)" +
		" AND EXISTS (SELECT 1 FROM workspaces w WHERE w.id = a.workspace_id AND w.deleted_at IS NULL)" +
		" ORDER BY a.id LIMIT ?"

	dropLeadDistrictsSQL = "DELETE FROM geo_district_points WHERE source = '" + georef.SourceLeads + "' AND built_at < ?"

	refineRunKey = "geocoding:district_refine:last"
	refineRunTTL = 72 * time.Hour
)

var (
	errRefinePageSize   = errors.New("georef refine: the page size must be positive")
	errRefineRunTime    = errors.New("georef refine: the run time is required")
	errRefineRunsNoSink = errors.New("georef refine: the run record needs shared state")
)

type Refinements struct {
	db *gorm.DB
}

var (
	_ georef.LocatedAddresses    = (*Refinements)(nil)
	_ georef.DistrictRefinements = (*Refinements)(nil)
)

func NewRefinements(db *gorm.DB) *Refinements {
	return &Refinements{db: db}
}

type locatedRow struct {
	ID           string
	WorkspaceID  string
	ZipCode      *string
	CityCode     *string
	City         *string
	State        *string
	District     *string
	Latitude     float64
	Longitude    float64
	GeoPrecision *string
	GeoSource    *string
}

func text(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func (r locatedRow) address() georef.LocatedAddress {
	return georef.LocatedAddress{
		ID:          r.ID,
		WorkspaceID: r.WorkspaceID,
		Postal: address.Postal{
			ZipCode: text(r.ZipCode), CityCode: text(r.CityCode), City: text(r.City), State: text(r.State), District: text(r.District),
		},
		Point:     geo.Point{Lat: r.Latitude, Lng: r.Longitude},
		Precision: geo.Precision(text(r.GeoPrecision)),
		Source:    geo.FixSource(text(r.GeoSource)),
	}
}

func refiningPrecisions() pq.StringArray {
	var out pq.StringArray
	for _, p := range georef.RefiningPrecisions() {
		out = append(out, string(p))
	}
	return out
}

func refiningSources() pq.StringArray {
	var out pq.StringArray
	for _, s := range georef.RefiningSources() {
		out = append(out, string(s))
	}
	return out
}

func (r *Refinements) LocatedAfter(ctx context.Context, afterID string, limit int) ([]georef.LocatedAddress, error) {
	if limit <= 0 {
		return nil, errRefinePageSize
	}
	after := strings.TrimSpace(afterID)
	if after == "" {
		after = firstAddressID
	}
	var rows []locatedRow
	if err := r.db.WithContext(ctx).Raw(locatedAfterSQL, after, refiningPrecisions(), refiningSources(), limit).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]georef.LocatedAddress, len(rows))
	for i, row := range rows {
		out[i] = row.address()
	}
	return out, nil
}

func (r *Refinements) RefineDistricts(ctx context.Context, points []georef.DistrictPoint, builtAt time.Time) error {
	if builtAt.IsZero() {
		return errRefineRunTime
	}
	return upsertDistricts(ctx, r.db, georef.SourceLeads, points, builtAt)
}

func (r *Refinements) DropLeadDistrictsBefore(ctx context.Context, builtAt time.Time) (int64, error) {
	if builtAt.IsZero() {
		return 0, errRefineRunTime
	}
	result := r.db.WithContext(ctx).Exec(dropLeadDistrictsSQL, builtAt)
	return result.RowsAffected, result.Error
}

type RefineRuns struct {
	state domainCache.SharedState
}

var _ georef.RefineRuns = (*RefineRuns)(nil)

func NewRefineRuns(state domainCache.SharedState) (*RefineRuns, error) {
	if state == nil {
		return nil, errRefineRunsNoSink
	}
	return &RefineRuns{state: state}, nil
}

func (r *RefineRuns) LastRefine(context.Context) (time.Time, error) {
	raw, err := r.state.GetString(refineRunKey)
	if err != nil {
		return time.Time{}, err
	}
	if raw == "" {
		return time.Time{}, nil
	}
	at, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return time.Time{}, fmt.Errorf("georef refine: unreadable last run %q: %w", raw, err)
	}
	return at, nil
}

func (r *RefineRuns) MarkRefine(_ context.Context, at time.Time) error {
	if at.IsZero() {
		return errRefineRunTime
	}
	return r.state.SetString(refineRunKey, at.UTC().Format(time.RFC3339Nano), refineRunTTL)
}
