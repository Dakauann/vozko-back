package georef_repository

import (
	"context"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/lib/pq"

	"vozko/domain/geo"
	"vozko/domain/georef"
)

func TestPlaceSQLCountsItsPlaceholders(t *testing.T) {
	for name, tc := range map[string]struct {
		sql          string
		placeholders int
	}{
		"cities":               {citySuggestSQL(""), 3},
		"cities of a state":    {citySuggestSQL(cityStateScope), 4},
		"bairros of a city":    {districtSuggestSQL(districtCityScope), 5},
		"bairros":              {districtSuggestSQL(""), 4},
		"bairros of a state":   {districtSuggestSQL(districtStateScope), 5},
		"streets of a city":    {streetSuggestSQL(streetCityScope), 4},
		"streets of a state":   {streetSuggestSQL(streetStateScope), 4},
		"streets":              {streetSuggestSQL(""), 3},
		"ceps":                 {cepSuggestSQL(""), 3},
		"ceps of a state":      {cepSuggestSQL(cepStateScope), 4},
		"ceps of a city":       {cepSuggestSQL(cepCityScope), 4},
		"cep streets":          {cepStreetsSQL, 1},
		"loads":                {loadsSQL, 0},
		"replace streets":      {upsertStreetsSQL, 9},
		"drop stale streets":   {dropStaleStreetsSQL, 2},
		"upsert cities":        {upsertCitiesSQL, 12},
		"upsert bairro points": {upsertDistrictsSQL, 14},
	} {
		if got := strings.Count(tc.sql, "?"); got != tc.placeholders {
			t.Fatalf("%s SQL has %d placeholders, want %d: %s", name, got, tc.placeholders, tc.sql)
		}
	}
	for _, sql := range []string{citySuggestSQL(""), districtSuggestSQL(""), streetSuggestSQL(""), cepSuggestSQL("")} {
		if !strings.Contains(sql, `LIKE ? ESCAPE '\'`) {
			t.Fatalf("a prefix search escapes its pattern: %s", sql)
		}
	}
	if !strings.Contains(districtSuggestSQL(""), "d.source = ?") {
		t.Fatal("bairro suggestions come from the census only")
	}
}

func TestCitySuggestionsBindTheEscapedPrefixAndTheState(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	q, _ := georef.ParsePlaceQuery("city", "rec_", "PE", "")
	mock.ExpectQuery(exact(citySuggestSQL(cityStateScope))).WithArgs(`rec\_%`, "PE", "rec_", 10).
		WillReturnRows(sqlmock.NewRows([]string{"city_code", "name", "name_key", "state", "latitude", "longitude", "bounds_south", "bounds_west", "bounds_north", "bounds_east", "address_count"}).
			AddRow("2611606", "Recife", "recife", "PE", -8.05, -34.9, -8.15, -35.0, -7.93, -34.85, 700000).
			AddRow("2611607", "Recife Sem Bordas", "recife sem bordas", "PE", -8.0, -34.8, nil, nil, nil, nil, 3))
	got, err := NewPlaces(db).Cities(context.Background(), q, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Kind != georef.PlaceCity || got[0].City != "Recife" || got[0].Key != "recife" || got[0].AddressCount != 700000 || got[0].Precision != geo.PrecisionCity {
		t.Fatalf("Cities() = %+v, want Recife", got)
	}
	if got[0].Bounds == nil || got[0].Bounds.South != -8.15 || got[0].Bounds.East != -34.85 {
		t.Fatalf("Cities() bounds = %+v, want the stored box", got[0].Bounds)
	}
	if got[1].Bounds != nil {
		t.Fatalf("a city without stored bounds = %+v, want none", got[1].Bounds)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestStreetSuggestionsCarryTheirCEPOnlyWhenTheStreetHasOne(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	q, _ := georef.ParsePlaceQuery("street", "Av. Boa", "", "2611606")
	columns := []string{"zip_code", "street_key", "street", "district", "district_key", "city_code", "zip_count", "address_count", "city", "state",
		"cep_latitude", "cep_longitude", "cep_spread_m", "district_latitude", "district_longitude", "city_latitude", "city_longitude"}
	mock.ExpectQuery(exact(streetSuggestSQL(streetCityScope))).WithArgs("boa", `boa%`, "2611606", 10).
		WillReturnRows(sqlmock.NewRows(columns).
			AddRow("51020230", "boa viagem", "Avenida Boa Viagem", "Boa Viagem", "boa viagem", "2611606", 12, 4000, "Recife", "PE", -8.1, -34.89, 900.0, -8.12, -34.9, -8.05, -34.9).
			AddRow("51030300", "boa hora", "Rua Boa Hora", "Pina", "pina", "2611606", 1, 30, "Recife", "PE", nil, nil, nil, -8.09, -34.88, -8.05, -34.9))
	got, err := NewPlaces(db).Streets(context.Background(), q, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("Streets() = %+v", got)
	}
	avenue, single := got[0], got[1]
	if avenue.ZipCode != "" || avenue.ZipCount != 12 || avenue.Street != "Avenida Boa Viagem" || avenue.District != "Boa Viagem" || avenue.City != "Recife" || avenue.State != "PE" {
		t.Fatalf("a street with 12 CEPs = %+v, want no CEP guessed", avenue)
	}
	if avenue.Precision != geo.PrecisionPostalCode || avenue.Point != (geo.Point{Lat: -8.1, Lng: -34.89}) {
		t.Fatalf("a street point = %+v %q, want its busiest CEP point", avenue.Point, avenue.Precision)
	}
	if single.ZipCode != "51030300" || single.Precision != geo.PrecisionDistrict || single.Point != (geo.Point{Lat: -8.09, Lng: -34.88}) {
		t.Fatalf("a street with one CEP and no CEP point = %+v, want its CEP and the bairro point", single)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestCEPSuggestionsReadTheirStreetsInOneArray(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	q, _ := georef.ParsePlaceQuery("", "51020-2", "", "")
	mock.ExpectQuery(exact(cepSuggestSQL(""))).WithArgs(`510202%`, "510202", 10).
		WillReturnRows(sqlmock.NewRows([]string{"zip_code", "latitude", "longitude", "spread_m", "address_count", "city_code", "city", "state"}).
			AddRow("51020230", -8.1, -34.89, 120.0, 300, "2611606", "Recife", "PE").
			AddRow("51020240", -8.11, -34.89, 120.0, 200, "2611606", "Recife", "PE"))
	mock.ExpectQuery(exact(cepStreetsSQL)).WithArgs(pq.StringArray{"51020230", "51020240"}).
		WillReturnRows(sqlmock.NewRows([]string{"zip_code", "street", "district", "city_code", "city", "state", "address_count"}).
			AddRow("51020230", "Avenida Boa Viagem", "Boa Viagem", "2611606", "Recife", "PE", 300).
			AddRow("51020240", "Rua A", "Boa Viagem", "2611606", "Recife", "PE", 100).
			AddRow("51020240", "Rua B", "Boa Viagem", "2611606", "Recife", "PE", 100))
	got, err := NewPlaces(db).CEPs(context.Background(), q, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Street != "Avenida Boa Viagem" || got[0].District != "Boa Viagem" || got[0].Precision != geo.PrecisionStreet {
		t.Fatalf("CEPs() = %+v, want the CEP with its only street", got)
	}
	if got[1].Street != "" || got[1].District != "Boa Viagem" || got[1].ZipCode != "51020240" {
		t.Fatalf("a CEP over two streets = %+v, want no street guessed", got[1])
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestCEPStreetsReadOneCEP(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	mock.ExpectQuery(exact(cepStreetsSQL)).WithArgs(pq.StringArray{"51020230"}).
		WillReturnRows(sqlmock.NewRows([]string{"zip_code", "street", "district", "city_code", "city", "state", "address_count"}).
			AddRow("51020230", "Avenida Boa Viagem", "Boa Viagem", "2611606", "Recife", "PE", 300))
	rows, err := NewPlaces(db).CEPStreets(context.Background(), "51020230")
	if err != nil || len(rows) != 1 || rows[0].City != "Recife" || rows[0].AddressCount != 300 {
		t.Fatalf("CEPStreets() = %+v, %v", rows, err)
	}
}

func TestLoadsReadEveryState(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	at := time.Date(2026, 10, 9, 2, 0, 0, 0, time.UTC)
	mock.ExpectQuery(exact(loadsSQL)).WillReturnRows(sqlmock.NewRows([]string{"state", "built_at"}).AddRow("PE", at).AddRow("DF", at))
	got, err := NewPlaces(db).Loads(context.Background())
	if err != nil || len(got) != 2 || got[0].State != "PE" || !got[0].BuiltAt.Equal(at) {
		t.Fatalf("Loads() = %+v, %v", got, err)
	}
}

func TestReplaceStreetsUpsertsInBatchesThenDropsWhatTheLoadNoLongerHas(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	builtAt := time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC)
	streets := make([]georef.Street, upsertBatch+1)
	for i := range streets {
		streets[i] = georef.Street{ZipCode: "51020230", CityCode: "2611606", Name: "Rua", Key: "rua " + strings.Repeat("a", i%5), DistrictKey: "pina", District: "Pina", AddressCount: 1}
	}
	any7 := []driver.Value{sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()}
	mock.ExpectExec(exact(upsertStreetsSQL)).WithArgs(append([]driver.Value{"PE", builtAt}, any7...)...).WillReturnResult(sqlmock.NewResult(0, upsertBatch))
	mock.ExpectExec(exact(upsertStreetsSQL)).WithArgs(append([]driver.Value{"PE", builtAt}, any7...)...).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(exact(dropStaleStreetsSQL)).WithArgs("PE", builtAt).WillReturnResult(sqlmock.NewResult(0, 4))
	if err := NewStore(db).ReplaceStreets(context.Background(), "PE", streets, builtAt); err != nil {
		t.Fatalf("ReplaceStreets() err = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestReplaceStreetsRefusesAMissingStateOrTime(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	store := NewStore(db)
	if err := store.ReplaceStreets(context.Background(), "", nil, time.Now()); !errors.Is(err, errStreetLoadIncomplete) {
		t.Fatalf("ReplaceStreets() without a state = %v", err)
	}
	if err := store.ReplaceStreets(context.Background(), "PE", nil, time.Time{}); !errors.Is(err, errStreetLoadIncomplete) {
		t.Fatalf("ReplaceStreets() without a load time = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
