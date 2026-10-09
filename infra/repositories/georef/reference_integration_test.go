package georef_repository

import (
	"context"
	"testing"
	"time"

	"vozko/domain/address"
	"vozko/domain/geo"
	"vozko/domain/georef"
	"vozko/infra/database/schema"
	"vozko/infra/repositories/repotest"
)

func TestReferenceRoundTripsAgainstPostgres(t *testing.T) {
	db := repotest.IsolatedDB(t, "georef", &schema.GeoCEPPoint{}, &schema.GeoCity{}, &schema.GeoDistrictPoint{}, &schema.GeoReferenceLoad{})
	ctx := context.Background()
	store, reference := NewStore(db), NewReference(db)
	builtAt := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)

	b := georef.NewBuilder(georef.DefaultLimits(), 1)
	for _, r := range []georef.Record{
		{CityCode: "1400100", ZipCode: "69301000", Locality: "CENTRO", Point: geo.Point{Lat: 2.8200, Lng: -60.6700}, Level: 1},
		{CityCode: "1400100", ZipCode: "69301000", Locality: "CENTRO", Point: geo.Point{Lat: 2.8202, Lng: -60.6702}, Level: 1},
		{CityCode: "1400100", ZipCode: "69318240", Locality: "CIDADE SATELITE", Point: geo.Point{Lat: 2.8500, Lng: -60.7200}, Level: 2},
	} {
		b.Add(r)
	}
	cities, missing := georef.Cities(b.CityPoints(), map[string]georef.Municipality{"1400100": {CityCode: "1400100", Name: "Boa Vista", State: "RR"}})
	if len(missing) != 0 {
		t.Fatalf("missing = %v", missing)
	}
	if err := store.UpsertCEPs(ctx, b.CEPPoints(), builtAt); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertDistricts(ctx, b.DistrictPoints(), builtAt); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertCities(ctx, cities, builtAt); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordLoad(ctx, georef.Load{State: "RR", Rows: 3, Measurement: georef.MeasureSpread(b.CEPPoints()), Districts: 2, Cities: 1, Source: georef.Attribution, BuiltAt: builtAt}); err != nil {
		t.Fatal(err)
	}

	coverage, err := reference.Coverage(ctx)
	if err != nil || !coverage.States["RR"] || coverage.Full() {
		t.Fatalf("Coverage() = %+v, %v, want RR only", coverage, err)
	}
	postals := []address.Postal{
		{ZipCode: "69301-000", District: "Centro", City: "Boa Vista", State: "RR"},
		{District: "Cidade Satélite", City: "BOA VISTA", State: "rr"},
	}
	idx, err := reference.Lookup(ctx, postals)
	if err != nil {
		t.Fatal(err)
	}
	first := idx.Candidates(postals[0], builtAt)
	if len(first) != 3 || first[0].Precision != geo.PrecisionCity || first[1].Precision != geo.PrecisionDistrict {
		t.Fatalf("generic CEP candidates = %+v, want the CEP as a city point, the bairro and the city", first)
	}
	second := idx.Candidates(postals[1], builtAt)
	if len(second) != 2 || second[0].Precision != geo.PrecisionDistrict || second[0].Point.Lat < 2.849 {
		t.Fatalf("bairro by name candidates = %+v, want Cidade Satelite found through the city name", second)
	}

	if err := db.Exec("UPDATE geo_district_points SET source = ?, latitude = 9 WHERE district_key = 'centro'", georef.SourceLeads).Error; err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertDistricts(ctx, b.DistrictPoints(), builtAt.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	var centro struct {
		Latitude float64
		Source   string
	}
	if err := db.Raw("SELECT latitude, source FROM geo_district_points WHERE district_key = 'centro'").Scan(&centro).Error; err != nil || centro.Latitude == 9 || centro.Source != georef.SourceCNEFE {
		t.Fatalf("centro = %+v, %v, want the census reload to replace the bairro point refined from leads", centro, err)
	}
}
