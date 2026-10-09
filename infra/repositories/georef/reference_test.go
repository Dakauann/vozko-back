package georef_repository

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/lib/pq"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"vozko/domain/address"
	"vozko/domain/geo"
	"vozko/domain/georef"
)

func newMockDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock, *sql.DB) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatal(err)
	}
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB, PreferSimpleProtocol: true, WithoutReturning: true}),
		&gorm.Config{SkipDefaultTransaction: true, Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	return db, mock, sqlDB
}

func exact(query string) string {
	quoted := regexp.QuoteMeta(query)
	var b strings.Builder
	n := 0
	for {
		before, after, found := strings.Cut(quoted, `\?`)
		b.WriteString(before)
		if !found {
			break
		}
		n++
		b.WriteString(`\$` + strconv.Itoa(n))
		quoted = after
	}
	return "^" + b.String() + "$"
}

func TestReferenceSQLBindsOneArrayPerKey(t *testing.T) {
	for name, tc := range map[string]struct {
		sql          string
		placeholders int
	}{
		"ceps":          {cepPointsSQL, 1},
		"city names":    {cityCodesByNameSQL, 2},
		"districts":     {districtPointsSQL, 2},
		"cities":        {cityPointsSQL, 1},
		"upsert ceps":   {upsertCEPsSQL, 8},
		"upsert places": {upsertDistrictsSQL, 14},
		"upsert cities": {upsertCitiesSQL, 12},
		"record load":   {recordLoadSQL, 8},
		"coverage":      {coverageSQL, 0},
	} {
		if got := strings.Count(tc.sql, "?"); got != tc.placeholders {
			t.Fatalf("%s SQL has %d placeholders, want %d: %s", name, got, tc.placeholders, tc.sql)
		}
	}
	if !strings.HasSuffix(upsertDistrictsSQL, ", source = excluded.source WHERE geo_district_points.source = ANY(?::text[])") {
		t.Fatalf("a bairro point is replaced only over the sources its writer may replace: %s", upsertDistrictsSQL)
	}
}

func TestLookupReadsInTwoPhases(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	postals := []address.Postal{
		{ZipCode: "01310100", District: "Bela Vista", City: "São Paulo", State: "SP"},
		{District: "Centro", City: "Contagem", State: "MG"},
	}
	mock.ExpectQuery(exact(cepPointsSQL)).WithArgs(sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"zip_code", "latitude", "longitude", "spread_m", "address_count", "city_code"}).
			AddRow("01310100", -23.561, -46.656, 120.0, 340, "3550308"))
	mock.ExpectQuery(exact(cityCodesByNameSQL)).WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"city_code", "state", "name_key"}).
			AddRow("3550308", "SP", "sao paulo").AddRow("3118601", "MG", "contagem"))
	mock.ExpectQuery(exact(districtPointsSQL)).WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"city_code", "district_key", "latitude", "longitude", "spread_m", "sample_count"}).
			AddRow("3118601", "centro", -19.93, -44.05, 900.0, 3000))
	mock.ExpectQuery(exact(cityPointsSQL)).WithArgs(sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"city_code", "latitude", "longitude"}).
			AddRow("3550308", -23.55, -46.63).AddRow("3118601", -19.92, -44.06))

	idx, err := NewReference(db).Lookup(context.Background(), postals)
	if err != nil {
		t.Fatalf("Lookup() err = %v", err)
	}
	got := idx.Candidates(postals[1], time.Time{})
	if len(got) != 2 || got[0].Precision != geo.PrecisionDistrict || got[1].Precision != geo.PrecisionCity {
		t.Fatalf("Contagem candidates = %+v, want its bairro then its city", got)
	}
	if first := idx.Candidates(postals[0], time.Time{}); len(first) != 2 || first[0].Precision != geo.PrecisionStreet {
		t.Fatalf("Paulista candidates = %+v, want the tight CEP and the city", first)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestLookupOfNothingReadsNothing(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	if _, err := NewReference(db).Lookup(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestCoverageNamesTheLoadedStates(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	mock.ExpectQuery(exact(coverageSQL)).WillReturnRows(sqlmock.NewRows([]string{"state"}).AddRow("SP").AddRow("RR"))
	got, err := NewReference(db).Coverage(context.Background())
	if err != nil || !got.States["SP"] || !got.States["RR"] || got.States["MG"] {
		t.Fatalf("Coverage() = %+v, %v", got, err)
	}
}

func TestUpsertsGoInBatches(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	points := make([]georef.CEPPoint, upsertBatch+1)
	for i := range points {
		points[i] = georef.CEPPoint{ZipCode: "0131" + strconv.Itoa(1000+i), CityCode: "3550308"}
	}
	builtAt := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
	any7 := []driver.Value{sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()}
	mock.ExpectExec(exact(upsertCEPsSQL)).WithArgs(append([]driver.Value{builtAt}, any7...)...).WillReturnResult(sqlmock.NewResult(0, upsertBatch))
	mock.ExpectExec(exact(upsertCEPsSQL)).WithArgs(append([]driver.Value{builtAt}, any7...)...).WillReturnResult(sqlmock.NewResult(0, 1))
	if err := NewStore(db).UpsertCEPs(context.Background(), points, builtAt); err != nil {
		t.Fatalf("UpsertCEPs() err = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestTheCensusReplacesBairroPointsRefinedFromLeads(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	builtAt := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
	points := []georef.DistrictPoint{{CityCode: "1400100", DistrictKey: "centro", Name: "Centro", Point: geo.Point{Lat: 2.82, Lng: -60.67}, SpreadM: 300, SampleCount: 4000}}
	mock.ExpectExec(exact(upsertDistrictsSQL)).WithArgs(georef.SourceCNEFE, builtAt,
		pq.StringArray{"1400100"}, pq.StringArray{"centro"}, pq.StringArray{"Centro"},
		pq.Float64Array{2.82}, pq.Float64Array{-60.67}, pq.Float64Array{300}, pq.Int64Array{4000},
		sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
		pq.StringArray{georef.SourceCNEFE, georef.SourceLeads}).
		WillReturnResult(sqlmock.NewResult(0, 1))
	if err := NewStore(db).UpsertDistricts(context.Background(), points, builtAt); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestABairroPointFromAnUnknownSourceIsNeverWritten(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	points := []georef.DistrictPoint{{CityCode: "1400100", DistrictKey: "centro", Name: "Centro"}}
	if err := upsertDistricts(context.Background(), db, "guess", points, time.Now()); err == nil {
		t.Fatal("upsertDistricts() wrote points from a source that replaces nothing")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
