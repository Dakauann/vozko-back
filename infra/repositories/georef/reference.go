package georef_repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/lib/pq"
	"gorm.io/gorm"

	"vozko/domain/address"
	"vozko/domain/geo"
	"vozko/domain/georef"
)

const upsertBatch = 1000

const (
	coverageSQL = "SELECT state FROM geo_reference_loads"

	cepPointsSQL = "SELECT zip_code, latitude, longitude, spread_m, address_count, city_code FROM geo_cep_points WHERE zip_code = ANY(?::text[])"

	cityCodesByNameSQL = "SELECT c.city_code, c.state, c.name_key FROM geo_cities c" +
		" JOIN unnest(?::text[], ?::text[]) AS k(state, name_key) ON c.state = k.state AND c.name_key = k.name_key"

	districtPointsSQL = "SELECT d.city_code, d.district_key, d.latitude, d.longitude, d.spread_m, d.sample_count FROM geo_district_points d" +
		" JOIN unnest(?::text[], ?::text[]) AS k(city_code, district_key) ON d.city_code = k.city_code AND d.district_key = k.district_key"

	cityPointsSQL = "SELECT city_code, latitude, longitude FROM geo_cities WHERE city_code = ANY(?::text[])"

	upsertCEPsSQL = "INSERT INTO geo_cep_points (zip_code, latitude, longitude, spread_m, address_count, sample_count, city_code, built_at)" +
		" SELECT u.zip_code, u.latitude, u.longitude, u.spread_m, u.address_count, u.sample_count, u.city_code, ?::timestamptz" +
		" FROM unnest(?::text[], ?::float8[], ?::float8[], ?::float8[], ?::bigint[], ?::bigint[], ?::text[])" +
		" AS u(zip_code, latitude, longitude, spread_m, address_count, sample_count, city_code)" +
		" ON CONFLICT (zip_code) DO UPDATE SET latitude = excluded.latitude, longitude = excluded.longitude, spread_m = excluded.spread_m," +
		" address_count = excluded.address_count, sample_count = excluded.sample_count, city_code = excluded.city_code, built_at = excluded.built_at"

	upsertDistrictsSQL = "INSERT INTO geo_district_points (city_code, district_key, name, latitude, longitude, spread_m, sample_count," +
		" bounds_south, bounds_west, bounds_north, bounds_east, source, built_at)" +
		" SELECT u.city_code, u.district_key, u.name, u.latitude, u.longitude, u.spread_m, u.sample_count," +
		" u.bounds_south, u.bounds_west, u.bounds_north, u.bounds_east, ?::text, ?::timestamptz" +
		" FROM unnest(?::text[], ?::text[], ?::text[], ?::float8[], ?::float8[], ?::float8[], ?::bigint[], ?::float8[], ?::float8[], ?::float8[], ?::float8[])" +
		" AS u(city_code, district_key, name, latitude, longitude, spread_m, sample_count, bounds_south, bounds_west, bounds_north, bounds_east)" +
		" ON CONFLICT (city_code, district_key) DO UPDATE SET name = excluded.name, latitude = excluded.latitude, longitude = excluded.longitude," +
		" spread_m = excluded.spread_m, sample_count = excluded.sample_count, bounds_south = excluded.bounds_south, bounds_west = excluded.bounds_west," +
		" bounds_north = excluded.bounds_north, bounds_east = excluded.bounds_east, built_at = excluded.built_at, source = excluded.source" +
		" WHERE geo_district_points.source = ANY(?::text[])"

	upsertCitiesSQL = "INSERT INTO geo_cities (city_code, name, name_key, state, latitude, longitude, bounds_south, bounds_west, bounds_north, bounds_east, address_count, built_at)" +
		" SELECT u.city_code, u.name, u.name_key, u.state, u.latitude, u.longitude, u.bounds_south, u.bounds_west, u.bounds_north, u.bounds_east, u.address_count, ?::timestamptz" +
		" FROM unnest(?::text[], ?::text[], ?::text[], ?::text[], ?::float8[], ?::float8[], ?::float8[], ?::float8[], ?::float8[], ?::float8[], ?::bigint[])" +
		" AS u(city_code, name, name_key, state, latitude, longitude, bounds_south, bounds_west, bounds_north, bounds_east, address_count)" +
		" ON CONFLICT (city_code) DO UPDATE SET name = excluded.name, name_key = excluded.name_key, state = excluded.state," +
		" latitude = excluded.latitude, longitude = excluded.longitude, bounds_south = excluded.bounds_south, bounds_west = excluded.bounds_west," +
		" bounds_north = excluded.bounds_north, bounds_east = excluded.bounds_east, address_count = excluded.address_count, built_at = excluded.built_at"

	recordLoadSQL = "INSERT INTO geo_reference_loads (state, rows, ceps, street_ceps, districts, cities, source, built_at)" +
		" VALUES (?, ?, ?, ?, ?, ?, ?, ?)" +
		" ON CONFLICT (state) DO UPDATE SET rows = excluded.rows, ceps = excluded.ceps, street_ceps = excluded.street_ceps," +
		" districts = excluded.districts, cities = excluded.cities, source = excluded.source, built_at = excluded.built_at"
)

var errDistrictSourceUnknown = errors.New("georef: bairro points from an unknown source are never written")

type Reference struct {
	db *gorm.DB
}

var _ geo.Reference = (*Reference)(nil)

func NewReference(db *gorm.DB) *Reference {
	return &Reference{db: db}
}

func (r *Reference) Coverage(ctx context.Context) (geo.Coverage, error) {
	var states []string
	if err := r.db.WithContext(ctx).Raw(coverageSQL).Scan(&states).Error; err != nil {
		return geo.Coverage{}, err
	}
	coverage := geo.Coverage{States: make(map[string]bool, len(states))}
	for _, s := range states {
		coverage.States[s] = true
	}
	return coverage, nil
}

type pointRow struct {
	ZipCode      string
	CityCode     string
	DistrictKey  string
	State        string
	NameKey      string
	Latitude     float64
	Longitude    float64
	SpreadM      float64
	AddressCount int64
	SampleCount  int64
}

func (row pointRow) point() geo.ReferencePoint {
	count := row.AddressCount
	if count == 0 {
		count = row.SampleCount
	}
	return geo.ReferencePoint{Point: geo.Point{Lat: row.Latitude, Lng: row.Longitude}, SpreadM: row.SpreadM, Count: count, CityCode: row.CityCode}
}

var _ georef.CityCodes = (*Reference)(nil)

func (r *Reference) CityCodes(ctx context.Context, postals []address.Postal) (geo.ReferenceIndex, error) {
	return cityCodesOf(r.db.WithContext(ctx), postals)
}

func (r *Reference) Lookup(ctx context.Context, postals []address.Postal) (geo.ReferenceIndex, error) {
	db := r.db.WithContext(ctx)
	index, err := cityCodesOf(db, postals)
	if err != nil {
		return geo.ReferenceIndex{}, err
	}
	return lookupPlaces(db, index, postals)
}

func cityCodesOf(db *gorm.DB, postals []address.Postal) (geo.ReferenceIndex, error) {
	keys := geo.ReferenceKeysFor(postals)
	index := geo.ReferenceIndex{
		CEPs: map[string]geo.ReferencePoint{}, Districts: map[address.Place]geo.ReferencePoint{},
		Cities: map[string]geo.ReferencePoint{}, CityCodes: map[geo.CityName]string{},
	}
	if len(keys.ZipCodes) > 0 {
		rows, err := scan(db, cepPointsSQL, pq.StringArray(keys.ZipCodes))
		if err != nil {
			return geo.ReferenceIndex{}, err
		}
		for _, row := range rows {
			index.CEPs[row.ZipCode] = row.point()
		}
	}
	if len(keys.CityNames) > 0 {
		states, names := make(pq.StringArray, len(keys.CityNames)), make(pq.StringArray, len(keys.CityNames))
		for i, n := range keys.CityNames {
			states[i], names[i] = n.State, n.NameKey
		}
		rows, err := scan(db, cityCodesByNameSQL, states, names)
		if err != nil {
			return geo.ReferenceIndex{}, err
		}
		for _, row := range rows {
			index.CityCodes[geo.CityName{State: row.State, NameKey: row.NameKey}] = row.CityCode
		}
	}
	return index, nil
}

func lookupPlaces(db *gorm.DB, index geo.ReferenceIndex, postals []address.Postal) (geo.ReferenceIndex, error) {
	places := index.PlacesFor(postals)
	if len(places.Districts) > 0 {
		codes, keys := make(pq.StringArray, len(places.Districts)), make(pq.StringArray, len(places.Districts))
		for i, p := range places.Districts {
			codes[i], keys[i] = p.CityCode, p.DistrictKey
		}
		rows, err := scan(db, districtPointsSQL, codes, keys)
		if err != nil {
			return geo.ReferenceIndex{}, err
		}
		for _, row := range rows {
			index.Districts[address.Place{CityCode: row.CityCode, DistrictKey: row.DistrictKey}] = row.point()
		}
	}
	if len(places.CityCodes) > 0 {
		rows, err := scan(db, cityPointsSQL, pq.StringArray(places.CityCodes))
		if err != nil {
			return geo.ReferenceIndex{}, err
		}
		for _, row := range rows {
			index.Cities[row.CityCode] = row.point()
		}
	}
	return index, nil
}

func scan(db *gorm.DB, sql string, args ...interface{}) ([]pointRow, error) {
	var rows []pointRow
	err := db.Raw(sql, args...).Scan(&rows).Error
	return rows, err
}

type Store struct {
	db *gorm.DB
}

var _ georef.Store = (*Store)(nil)

func NewStore(db *gorm.DB) *Store {
	return &Store{db: db}
}

func inBatches[T any](items []T, write func([]T) error) error {
	for start := 0; start < len(items); start += upsertBatch {
		if err := write(items[start:min(start+upsertBatch, len(items))]); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) UpsertCEPs(ctx context.Context, points []georef.CEPPoint, builtAt time.Time) error {
	return inBatches(points, func(batch []georef.CEPPoint) error {
		var zips, cities pq.StringArray
		var lats, lngs, spreads pq.Float64Array
		var counts, samples pq.Int64Array
		for _, p := range batch {
			zips, cities = append(zips, p.ZipCode), append(cities, p.CityCode)
			lats, lngs, spreads = append(lats, p.Point.Lat), append(lngs, p.Point.Lng), append(spreads, p.SpreadM)
			counts, samples = append(counts, p.AddressCount), append(samples, p.SampleCount)
		}
		return s.db.WithContext(ctx).Exec(upsertCEPsSQL, builtAt, zips, lats, lngs, spreads, counts, samples, cities).Error
	})
}

func (s *Store) UpsertDistricts(ctx context.Context, points []georef.DistrictPoint, builtAt time.Time) error {
	return upsertDistricts(ctx, s.db, georef.SourceCNEFE, points, builtAt)
}

func upsertDistricts(ctx context.Context, db *gorm.DB, source string, points []georef.DistrictPoint, builtAt time.Time) error {
	replaced := georef.DistrictSourcesReplacedBy(source)
	if len(replaced) == 0 {
		return fmt.Errorf("%w: %q", errDistrictSourceUnknown, source)
	}
	return inBatches(points, func(batch []georef.DistrictPoint) error {
		cities, keys, names, lats, lngs, spreads, samples := districtColumns(batch)
		var bounds boundsColumns
		for _, p := range batch {
			bounds.add(p.Bounds)
		}
		return db.WithContext(ctx).Exec(upsertDistrictsSQL, source, builtAt, cities, keys, names, lats, lngs, spreads, samples,
			bounds.south(), bounds.west(), bounds.north(), bounds.east(), pq.StringArray(replaced)).Error
	})
}

func districtColumns(batch []georef.DistrictPoint) (cities, keys, names pq.StringArray, lats, lngs, spreads pq.Float64Array, samples pq.Int64Array) {
	for _, p := range batch {
		cities, keys, names = append(cities, p.CityCode), append(keys, p.DistrictKey), append(names, p.Name)
		lats, lngs, spreads = append(lats, p.Point.Lat), append(lngs, p.Point.Lng), append(spreads, p.SpreadM)
		samples = append(samples, p.SampleCount)
	}
	return cities, keys, names, lats, lngs, spreads, samples
}

type boundsColumns struct {
	sides [4][]sql.NullFloat64
}

func (c *boundsColumns) add(b geo.BBox) {
	known := b != (geo.BBox{}) && b.Validate() == nil
	for i, v := range []float64{b.South, b.West, b.North, b.East} {
		c.sides[i] = append(c.sides[i], sql.NullFloat64{Float64: v, Valid: known})
	}
}

func (c *boundsColumns) south() interface{} { return pq.Array(c.sides[0]) }
func (c *boundsColumns) west() interface{}  { return pq.Array(c.sides[1]) }
func (c *boundsColumns) north() interface{} { return pq.Array(c.sides[2]) }
func (c *boundsColumns) east() interface{}  { return pq.Array(c.sides[3]) }

func (s *Store) UpsertCities(ctx context.Context, cities []georef.City, builtAt time.Time) error {
	return inBatches(cities, func(batch []georef.City) error {
		var codes, names, nameKeys, states pq.StringArray
		var lats, lngs pq.Float64Array
		var counts pq.Int64Array
		var bounds boundsColumns
		for _, c := range batch {
			codes, names, nameKeys, states = append(codes, c.CityCode), append(names, c.Name), append(nameKeys, c.NameKey), append(states, c.State)
			lats, lngs, counts = append(lats, c.Point.Lat), append(lngs, c.Point.Lng), append(counts, c.AddressCount)
			bounds.add(c.Bounds)
		}
		return s.db.WithContext(ctx).Exec(upsertCitiesSQL, builtAt, codes, names, nameKeys, states, lats, lngs,
			bounds.south(), bounds.west(), bounds.north(), bounds.east(), counts).Error
	})
}

func (s *Store) RecordLoad(ctx context.Context, load georef.Load) error {
	return s.db.WithContext(ctx).Exec(recordLoadSQL,
		load.State, load.Rows, int64(load.Measurement.CEPs), int64(load.Measurement.Street),
		int64(load.Districts), int64(load.Cities), load.Source, load.BuiltAt,
	).Error
}
