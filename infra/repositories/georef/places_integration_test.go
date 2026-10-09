package georef_repository

import (
	"context"
	"strings"
	"testing"
	"time"

	"vozko/domain/geo"
	"vozko/domain/georef"
	"vozko/infra/database"
	"vozko/infra/database/schema"
	"vozko/infra/repositories/repotest"
)

func placeTables() []interface{} {
	return []interface{}{&schema.GeoCEPPoint{}, &schema.GeoCity{}, &schema.GeoDistrictPoint{}, &schema.GeoCEPStreet{}, &schema.GeoReferenceLoad{}}
}

func buildPlaceIndexes(t *testing.T, exec func(string) error) {
	t.Helper()
	for _, name := range database.PlaceSearchIndexNames() {
		sql, ok := database.ConcurrentIndexSQL(name)
		if !ok {
			t.Fatalf("%s has no SQL", name)
		}
		if err := exec(strings.Replace(sql, "CONCURRENTLY ", "", 1)); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
}

func loadRecife(t *testing.T, store *Store, records []georef.Record, builtAt time.Time) {
	t.Helper()
	ctx := context.Background()
	b := georef.NewBuilder(georef.DefaultLimits(), 1)
	for _, r := range records {
		b.Add(r)
	}
	cities, _ := georef.Cities(b.CityPoints(), map[string]georef.Municipality{"2611606": {CityCode: "2611606", Name: "Recife", State: "PE"}})
	for _, write := range []func() error{
		func() error { return store.UpsertCEPs(ctx, b.CEPPoints(), builtAt) },
		func() error { return store.UpsertDistricts(ctx, b.DistrictPoints(), builtAt) },
		func() error { return store.UpsertCities(ctx, cities, builtAt) },
		func() error { return store.ReplaceStreets(ctx, "PE", b.Streets(), builtAt) },
		func() error {
			return store.RecordLoad(ctx, georef.Load{State: "PE", Rows: int64(len(records)), Source: georef.Attribution, BuiltAt: builtAt})
		},
	} {
		if err := write(); err != nil {
			t.Fatal(err)
		}
	}
}

func recifeRow(zip, locality, kind, name string, lat, lng float64) georef.Record {
	return georef.Record{CityCode: "2611606", ZipCode: zip, Locality: locality, StreetKind: kind, StreetName: name, Point: geo.Point{Lat: lat, Lng: lng}, Level: 1}
}

func TestPlaceSuggestionsRoundTripAgainstPostgres(t *testing.T) {
	db := repotest.IsolatedDB(t, "georef_places", placeTables()...)
	buildPlaceIndexes(t, func(sql string) error { return db.Exec(sql).Error })
	ctx := context.Background()
	store, places := NewStore(db), NewPlaces(db)
	builtAt := time.Date(2026, 10, 9, 2, 0, 0, 0, time.UTC)
	records := []georef.Record{
		recifeRow("51020230", "BOA VIAGEM", "AVENIDA", "BOA VIAGEM", -8.1200, -34.8950),
		recifeRow("51020230", "BOA VIAGEM", "AVENIDA", "BOA VIAGEM", -8.1201, -34.8951),
		recifeRow("51021000", "BOA VIAGEM", "AVENIDA", "BOA VIAGEM", -8.1300, -34.8990),
		recifeRow("51030300", "PINA", "RUA", "BOA HORA", -8.0900, -34.8800),
		recifeRow("51030310", "PINA", "RUA", "SEM DENOMINACAO", -8.0910, -34.8810),
	}
	loadRecife(t, store, records, builtAt)

	stamps, err := places.Loads(ctx)
	if err != nil || len(stamps) != 1 || stamps[0].State != "PE" {
		t.Fatalf("Loads() = %+v, %v", stamps, err)
	}

	cityQuery, _ := georef.ParsePlaceQuery("city", "rec", "PE", "")
	cities, err := places.Cities(ctx, cityQuery, georef.PlaceLimit)
	if err != nil || len(cities) != 1 || cities[0].City != "Recife" || cities[0].Bounds == nil || cities[0].AddressCount != 5 {
		t.Fatalf("Cities() = %+v, %v, want Recife with its bounds and its 5 addresses", cities, err)
	}

	districtQuery, _ := georef.ParsePlaceQuery("district", "boa", "", "2611606")
	districts, err := places.Districts(ctx, districtQuery, georef.PlaceLimit)
	if err != nil || len(districts) != 1 || districts[0].District != "Boa Viagem" || districts[0].City != "Recife" || districts[0].Bounds == nil {
		t.Fatalf("Districts() = %+v, %v, want Boa Viagem in Recife", districts, err)
	}

	streetQuery, _ := georef.ParsePlaceQuery("street", "av boa", "", "2611606")
	streets, err := places.Streets(ctx, streetQuery, georef.PlaceLimit)
	if err != nil || len(streets) != 2 {
		t.Fatalf("Streets() = %+v, %v, want the avenue and Rua Boa Hora", streets, err)
	}
	if streets[0].Street != "Avenida Boa Viagem" || streets[0].ZipCount != 2 || streets[0].ZipCode != "" || streets[0].AddressCount != 3 {
		t.Fatalf("avenue = %+v, want both of its CEPs counted and none guessed", streets[0])
	}
	if streets[1].Street != "Rua Boa Hora" || streets[1].ZipCode != "51030300" || streets[1].District != "Pina" {
		t.Fatalf("street = %+v, want its only CEP and its bairro", streets[1])
	}

	cepQuery, _ := georef.ParsePlaceQuery("", "51020", "", "")
	ceps, err := places.CEPs(ctx, cepQuery, georef.PlaceLimit)
	if err != nil || len(ceps) != 1 || ceps[0].ZipCode != "51020230" || ceps[0].Street != "Avenida Boa Viagem" || ceps[0].City != "Recife" {
		t.Fatalf("CEPs() = %+v, %v, want 51020230 alone with its street", ceps, err)
	}

	rows, err := places.CEPStreets(ctx, "51030300")
	if err != nil || len(rows) != 1 || rows[0].Street != "Rua Boa Hora" || rows[0].State != "PE" {
		t.Fatalf("CEPStreets() = %+v, %v", rows, err)
	}

	loadRecife(t, store, records[:3], builtAt.Add(time.Hour))
	var left int64
	if err := db.Raw("SELECT count(*) FROM geo_cep_streets").Scan(&left).Error; err != nil || left != 2 {
		t.Fatalf("after a reload without Rua Boa Hora, %d street rows remain (%v), want 2", left, err)
	}
	loadRecife(t, store, records[:3], builtAt.Add(time.Hour))
	if err := db.Raw("SELECT count(*) FROM geo_cep_streets").Scan(&left).Error; err != nil || left != 2 {
		t.Fatalf("a repeated load left %d street rows (%v), want 2", left, err)
	}
}

func TestPlaceSuggestionsUseThePrefixIndexesAgainstPostgres(t *testing.T) {
	db := repotest.IsolatedDB(t, "georef_places_volume", placeTables()...)
	seed := []string{
		`INSERT INTO geo_cities (city_code, name, name_key, state, latitude, longitude, address_count, built_at)
			SELECT lpad((2600000 + g)::text, 7, '0'), 'Cidade ' || g, 'cidade ' || md5(g::text), 'PE', -8, -35, g, now()
			FROM generate_series(1, 6000) g`,
		`INSERT INTO geo_district_points (city_code, district_key, name, latitude, longitude, spread_m, sample_count, source, built_at)
			SELECT lpad((2600000 + (g % 6000) + 1)::text, 7, '0'), md5(g::text), 'Bairro ' || g, -8, -35, 100, g, 'cnefe', now()
			FROM generate_series(1, 60000) g`,
		`INSERT INTO geo_cep_points (zip_code, latitude, longitude, spread_m, address_count, sample_count, city_code, built_at)
			SELECT lpad((50000000 + g)::text, 8, '0'), -8, -35, 100, g, g, lpad((2600000 + (g % 6000) + 1)::text, 7, '0'), now()
			FROM generate_series(1, 300000) g`,
		`INSERT INTO geo_cep_streets (zip_code, city_code, street_key, district_key, state, street, district, address_count, built_at)
			SELECT lpad((50000000 + (g % 300000) + 1)::text, 8, '0'), lpad((2600000 + (g % 6000) + 1)::text, 7, '0'),
				md5(g::text), md5((g % 60000)::text), 'PE', 'Rua ' || g, 'Bairro', g % 97, now()
			FROM generate_series(1, 300000) g`,
		`INSERT INTO geo_reference_loads (state, rows, ceps, street_ceps, districts, cities, source, built_at) VALUES ('PE', 1, 1, 1, 1, 1, 'IBGE', now())`,
	}
	for _, sql := range seed {
		if err := db.Exec(sql).Error; err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	buildPlaceIndexes(t, func(sql string) error { return db.Exec(sql).Error })
	if err := db.Exec("ANALYZE").Error; err != nil {
		t.Fatal(err)
	}
	var cityCode string
	if err := db.Raw("SELECT city_code FROM geo_cities ORDER BY city_code LIMIT 1").Scan(&cityCode).Error; err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name  string
		sql   string
		args  []interface{}
		index string
	}{
		{"cities", citySuggestSQL(cityStateScope), []interface{}{"cidade a%", "PE", "cidade a", 10}, database.GeoCityPrefixIndex},
		{"bairros of a city", districtSuggestSQL(districtCityScope), []interface{}{georef.SourceCNEFE, "a%", cityCode, "a", 10}, database.GeoDistrictCityPrefixIndex},
		{"bairros", districtSuggestSQL(""), []interface{}{georef.SourceCNEFE, "ab%", "ab", 10}, database.GeoDistrictPrefixIndex},
		{"streets of a city", streetSuggestSQL(streetCityScope), []interface{}{"a", "a%", cityCode, 10}, database.GeoCEPStreetCityPrefixIndex},
		{"streets", streetSuggestSQL(""), []interface{}{"ab", "ab%", 10}, database.GeoCEPStreetPrefixIndex},
		{"ceps", cepSuggestSQL(""), []interface{}{"50012%", "50012", 10}, database.GeoCEPPrefixIndex},
	}
	for _, tc := range cases {
		var lines []string
		if err := db.Raw("EXPLAIN (ANALYZE, BUFFERS) "+tc.sql, tc.args...).Scan(&lines).Error; err != nil {
			t.Fatalf("%s: explain: %v", tc.name, err)
		}
		plan := strings.Join(lines, "\n")
		if !strings.Contains(plan, tc.index) {
			t.Fatalf("%s does not read %s:\n%s", tc.name, tc.index, plan)
		}
		t.Logf("%s: %s", tc.name, plan)
	}
}
