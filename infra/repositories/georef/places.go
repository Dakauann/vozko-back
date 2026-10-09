package georef_repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/lib/pq"
	"gorm.io/gorm"

	"vozko/domain/geo"
	"vozko/domain/georef"
	"vozko/infra/database"
)

const (
	cityStateScope     = " AND c.state = ?"
	districtCityScope  = " AND d.city_code = ?"
	districtStateScope = " AND c.state = ?"
	streetCityScope    = " AND s.city_code = ?"
	streetStateScope   = " AND s.state = ?"
	cepCityScope       = " AND p.city_code = ?"
	cepStateScope      = " AND c.state = ?"

	loadsSQL = "SELECT state, built_at FROM geo_reference_loads"

	cepStreetsSQL = "SELECT s.zip_code, s.street, s.district, s.city_code, c.name AS city, c.state, s.address_count" +
		" FROM geo_cep_streets s JOIN geo_cities c ON c.city_code = s.city_code WHERE s.zip_code = ANY(?::text[])"

	upsertStreetsSQL = "INSERT INTO geo_cep_streets (zip_code, city_code, street_key, district_key, state, street, district, address_count, built_at)" +
		" SELECT u.zip_code, u.city_code, u.street_key, u.district_key, ?::text, u.street, u.district, u.address_count, ?::timestamptz" +
		" FROM unnest(?::text[], ?::text[], ?::text[], ?::text[], ?::text[], ?::text[], ?::bigint[])" +
		" AS u(zip_code, city_code, street_key, district_key, street, district, address_count)" +
		" ON CONFLICT (zip_code, city_code, street_key, district_key) DO UPDATE SET state = excluded.state, street = excluded.street," +
		" district = excluded.district, address_count = excluded.address_count, built_at = excluded.built_at"

	dropStaleStreetsSQL = "DELETE FROM geo_cep_streets WHERE state = ? AND built_at < ?"
)

var errStreetLoadIncomplete = errors.New("georef: a street load names its state and its load time")

func citySuggestSQL(scope string) string {
	return "SELECT c.city_code, c.name, c.name_key, c.state, c.latitude, c.longitude," +
		" c.bounds_south, c.bounds_west, c.bounds_north, c.bounds_east, c.address_count" +
		" FROM geo_cities c WHERE c.name_key LIKE ? ESCAPE '\\'" + scope +
		" ORDER BY (c.name_key = ?) DESC, c.address_count DESC, c.name LIMIT ?"
}

func districtSuggestSQL(scope string) string {
	return "SELECT d.city_code, d.district_key, d.name, d.latitude, d.longitude," +
		" d.bounds_south, d.bounds_west, d.bounds_north, d.bounds_east, d.sample_count AS address_count, c.name AS city, c.state" +
		" FROM geo_district_points d JOIN geo_cities c ON c.city_code = d.city_code" +
		" WHERE d.source = ? AND d.district_key LIKE ? ESCAPE '\\'" + scope +
		" ORDER BY (d.district_key = ?) DESC, d.sample_count DESC, d.name LIMIT ?"
}

func streetSuggestSQL(scope string) string {
	return "SELECT g.zip_code, g.street_key, g.street, g.district, g.district_key, g.city_code, g.zip_count, g.address_count," +
		" c.name AS city, c.state, p.latitude AS cep_latitude, p.longitude AS cep_longitude, p.spread_m AS cep_spread_m," +
		" d.latitude AS district_latitude, d.longitude AS district_longitude, c.latitude AS city_latitude, c.longitude AS city_longitude" +
		" FROM (SELECT grouped.*, (grouped.street_key = ?) AS exact FROM" +
		" (SELECT DISTINCT ON (s.city_code, s.street_key, s.district_key) s.zip_code, s.street_key, s.street, s.district, s.district_key, s.city_code," +
		" count(*) OVER w AS zip_count, (sum(s.address_count) OVER w)::bigint AS address_count" +
		" FROM geo_cep_streets s WHERE s.street_key LIKE ? ESCAPE '\\'" + scope +
		" WINDOW w AS (PARTITION BY s.city_code, s.street_key, s.district_key)" +
		" ORDER BY s.city_code, s.street_key, s.district_key, s.address_count DESC, s.zip_code) grouped" +
		" ORDER BY exact DESC, grouped.address_count DESC, grouped.street, grouped.zip_code LIMIT ?) g" +
		" JOIN geo_cities c ON c.city_code = g.city_code" +
		" LEFT JOIN geo_cep_points p ON p.zip_code = g.zip_code" +
		" LEFT JOIN geo_district_points d ON d.city_code = g.city_code AND d.district_key = g.district_key" +
		" ORDER BY g.exact DESC, g.address_count DESC, g.street, g.zip_code"
}

func cepSuggestSQL(scope string) string {
	return "SELECT p.zip_code, p.latitude, p.longitude, p.spread_m, p.address_count, p.city_code, c.name AS city, c.state" +
		" FROM geo_cep_points p JOIN geo_cities c ON c.city_code = p.city_code" +
		" WHERE p.zip_code LIKE ? ESCAPE '\\'" + scope +
		" ORDER BY (p.zip_code = ?) DESC, p.address_count DESC, p.zip_code LIMIT ?"
}

type Places struct {
	db *gorm.DB
}

var (
	_ georef.PlaceIndex = (*Places)(nil)
	_ georef.CEPStreets = (*Places)(nil)
)

func NewPlaces(db *gorm.DB) *Places {
	return &Places{db: db}
}

func (p *Places) Loads(ctx context.Context) ([]georef.LoadStamp, error) {
	var rows []struct {
		State   string
		BuiltAt time.Time
	}
	if err := p.db.WithContext(ctx).Raw(loadsSQL).Scan(&rows).Error; err != nil {
		return nil, err
	}
	stamps := make([]georef.LoadStamp, len(rows))
	for i, r := range rows {
		stamps[i] = georef.LoadStamp{State: r.State, BuiltAt: r.BuiltAt}
	}
	return stamps, nil
}

type BoundsRow struct {
	BoundsSouth sql.NullFloat64
	BoundsWest  sql.NullFloat64
	BoundsNorth sql.NullFloat64
	BoundsEast  sql.NullFloat64
}

func (r BoundsRow) bounds() *geo.BBox {
	if !r.BoundsSouth.Valid || !r.BoundsWest.Valid || !r.BoundsNorth.Valid || !r.BoundsEast.Valid {
		return nil
	}
	b := geo.BBox{South: r.BoundsSouth.Float64, West: r.BoundsWest.Float64, North: r.BoundsNorth.Float64, East: r.BoundsEast.Float64}
	if b.Validate() != nil {
		return nil
	}
	return &b
}

func referenceFix(lat, lng sql.NullFloat64, precision geo.Precision) []geo.Fix {
	if !lat.Valid || !lng.Valid {
		return nil
	}
	return []geo.Fix{{Point: geo.Point{Lat: lat.Float64, Lng: lng.Float64}, Precision: precision, Source: geo.SourceReference}}
}

func scoped(q georef.PlaceQuery, city, state string) (string, []interface{}) {
	switch {
	case q.CityCode != "" && city != "":
		return city, []interface{}{q.CityCode}
	case q.State != "":
		return state, []interface{}{q.State}
	}
	return "", nil
}

func (p *Places) Cities(ctx context.Context, q georef.PlaceQuery, limit int) ([]georef.Place, error) {
	scope, scopeArgs := scoped(q, "", cityStateScope)
	args := append(append([]interface{}{database.LikePrefix(q.Key)}, scopeArgs...), q.Key, limit)
	var rows []struct {
		CityCode     string
		Name         string
		NameKey      string
		State        string
		Latitude     float64
		Longitude    float64
		AddressCount int64
		BoundsRow
	}
	if err := p.db.WithContext(ctx).Raw(citySuggestSQL(scope), args...).Scan(&rows).Error; err != nil {
		return nil, err
	}
	places := make([]georef.Place, len(rows))
	for i, r := range rows {
		places[i] = georef.Place{
			Kind: georef.PlaceCity, Key: r.NameKey, Name: r.Name, City: r.Name, CityCode: r.CityCode, State: r.State,
			Point: geo.Point{Lat: r.Latitude, Lng: r.Longitude}, Precision: geo.PrecisionCity, Bounds: r.bounds(), AddressCount: r.AddressCount,
		}
	}
	return places, nil
}

func (p *Places) Districts(ctx context.Context, q georef.PlaceQuery, limit int) ([]georef.Place, error) {
	scope, scopeArgs := scoped(q, districtCityScope, districtStateScope)
	args := append(append([]interface{}{georef.SourceCNEFE, database.LikePrefix(q.Key)}, scopeArgs...), q.Key, limit)
	var rows []struct {
		CityCode     string
		DistrictKey  string
		Name         string
		Latitude     float64
		Longitude    float64
		AddressCount int64
		City         string
		State        string
		BoundsRow
	}
	if err := p.db.WithContext(ctx).Raw(districtSuggestSQL(scope), args...).Scan(&rows).Error; err != nil {
		return nil, err
	}
	places := make([]georef.Place, len(rows))
	for i, r := range rows {
		name := georef.PlaceDisplay(r.Name)
		places[i] = georef.Place{
			Kind: georef.PlaceDistrict, Key: r.DistrictKey, Name: name, District: name, City: r.City, CityCode: r.CityCode, State: r.State,
			Point: geo.Point{Lat: r.Latitude, Lng: r.Longitude}, Precision: geo.PrecisionDistrict, Bounds: r.bounds(), AddressCount: r.AddressCount,
		}
	}
	return places, nil
}

func (p *Places) Streets(ctx context.Context, q georef.PlaceQuery, limit int) ([]georef.Place, error) {
	scope, scopeArgs := scoped(q, streetCityScope, streetStateScope)
	args := append(append([]interface{}{q.StreetKey, database.LikePrefix(q.StreetKey)}, scopeArgs...), limit)
	var rows []struct {
		ZipCode           string
		StreetKey         string
		Street            string
		District          string
		DistrictKey       string
		CityCode          string
		ZipCount          int64
		AddressCount      int64
		City              string
		State             string
		CEPLatitude       sql.NullFloat64 `gorm:"column:cep_latitude"`
		CEPLongitude      sql.NullFloat64 `gorm:"column:cep_longitude"`
		CEPSpreadM        sql.NullFloat64 `gorm:"column:cep_spread_m"`
		DistrictLatitude  sql.NullFloat64
		DistrictLongitude sql.NullFloat64
		CityLatitude      sql.NullFloat64
		CityLongitude     sql.NullFloat64
	}
	if err := p.db.WithContext(ctx).Raw(streetSuggestSQL(scope), args...).Scan(&rows).Error; err != nil {
		return nil, err
	}
	places := make([]georef.Place, 0, len(rows))
	for _, r := range rows {
		var fixes []geo.Fix
		if r.CEPSpreadM.Valid {
			fixes = append(fixes, referenceFix(r.CEPLatitude, r.CEPLongitude, geo.CEPPrecision(r.ZipCode, r.CEPSpreadM.Float64))...)
		}
		fixes = append(fixes, referenceFix(r.DistrictLatitude, r.DistrictLongitude, geo.PrecisionDistrict)...)
		fixes = append(fixes, referenceFix(r.CityLatitude, r.CityLongitude, geo.PrecisionCity)...)
		point, precision, ok := georef.BestPoint(fixes...)
		if !ok {
			continue
		}
		place := georef.Place{
			Kind: georef.PlaceStreet, Key: r.StreetKey, Name: r.Street, Street: r.Street, District: r.District,
			City: r.City, CityCode: r.CityCode, State: r.State, Point: point, Precision: precision,
			AddressCount: r.AddressCount, ZipCount: r.ZipCount,
		}
		if r.ZipCount == 1 {
			place.ZipCode = r.ZipCode
		}
		places = append(places, place)
	}
	return places, nil
}

func (p *Places) CEPs(ctx context.Context, q georef.PlaceQuery, limit int) ([]georef.Place, error) {
	scope, scopeArgs := scoped(q, cepCityScope, cepStateScope)
	args := append(append([]interface{}{database.LikePrefix(q.ZipPrefix)}, scopeArgs...), q.ZipPrefix, limit)
	var rows []struct {
		ZipCode      string
		Latitude     float64
		Longitude    float64
		SpreadM      float64
		AddressCount int64
		CityCode     string
		City         string
		State        string
	}
	db := p.db.WithContext(ctx)
	if err := db.Raw(cepSuggestSQL(scope), args...).Scan(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return []georef.Place{}, nil
	}
	zips := make(pq.StringArray, len(rows))
	for i, r := range rows {
		zips[i] = r.ZipCode
	}
	streets, err := cepStreetRows(db, zips)
	if err != nil {
		return nil, err
	}
	places := make([]georef.Place, len(rows))
	for i, r := range rows {
		info, _ := georef.CEPInfoFromStreets(r.ZipCode, streets[r.ZipCode])
		places[i] = georef.Place{
			Kind: georef.PlaceCEP, Key: r.ZipCode, Name: r.ZipCode, ZipCode: r.ZipCode, Street: info.Logradouro, District: info.Bairro,
			City: r.City, CityCode: r.CityCode, State: r.State, Point: geo.Point{Lat: r.Latitude, Lng: r.Longitude},
			Precision: geo.CEPPrecision(r.ZipCode, r.SpreadM), AddressCount: r.AddressCount, ZipCount: 1,
		}
	}
	return places, nil
}

func (p *Places) CEPStreets(ctx context.Context, zip string) ([]georef.CEPStreetRow, error) {
	rows, err := cepStreetRows(p.db.WithContext(ctx), pq.StringArray{zip})
	if err != nil {
		return nil, err
	}
	return rows[zip], nil
}

func cepStreetRows(db *gorm.DB, zips pq.StringArray) (map[string][]georef.CEPStreetRow, error) {
	var rows []struct {
		ZipCode string
		georef.CEPStreetRow
	}
	if err := db.Raw(cepStreetsSQL, zips).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[string][]georef.CEPStreetRow, len(zips))
	for _, r := range rows {
		out[r.ZipCode] = append(out[r.ZipCode], r.CEPStreetRow)
	}
	return out, nil
}

func (s *Store) ReplaceStreets(ctx context.Context, state string, streets []georef.Street, builtAt time.Time) error {
	state = strings.TrimSpace(state)
	if state == "" || builtAt.IsZero() {
		return errStreetLoadIncomplete
	}
	db := s.db.WithContext(ctx)
	err := inBatches(streets, func(batch []georef.Street) error {
		var zips, cities, keys, districtKeys, names, districts pq.StringArray
		var counts pq.Int64Array
		for _, st := range batch {
			zips, cities, keys = append(zips, st.ZipCode), append(cities, st.CityCode), append(keys, st.Key)
			districtKeys, names, districts = append(districtKeys, st.DistrictKey), append(names, st.Name), append(districts, st.District)
			counts = append(counts, st.AddressCount)
		}
		return db.Exec(upsertStreetsSQL, state, builtAt, zips, cities, keys, districtKeys, names, districts, counts).Error
	})
	if err != nil {
		return err
	}
	if err := db.Exec(dropStaleStreetsSQL, state, builtAt).Error; err != nil {
		return fmt.Errorf("georef: drop the streets the %s load no longer has: %w", state, err)
	}
	return nil
}
