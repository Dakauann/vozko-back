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

	"vozko/domain/address"
	domainCache "vozko/domain/cache"
	"vozko/domain/geo"
	"vozko/domain/georef"
)

func TestRefineStatementsBindEveryPlaceholder(t *testing.T) {
	for name, tc := range map[string]struct {
		sql          string
		placeholders int
	}{
		"located after": {locatedAfterSQL, 4},
		"drop stale":    {dropLeadDistrictsSQL, 1},
		"upsert places": {upsertDistrictsSQL, 14},
	} {
		if got := strings.Count(tc.sql, "?"); got != tc.placeholders {
			t.Fatalf("%s SQL has %d placeholders, want %d: %s", name, got, tc.placeholders, tc.sql)
		}
	}
	if !strings.Contains(dropLeadDistrictsSQL, "source = '"+georef.SourceLeads+"' AND built_at < ?") {
		t.Fatalf("only stale bairro points refined from leads may be dropped: %s", dropLeadDistrictsSQL)
	}
	for _, want := range []string{
		"a.id > ?::uuid",
		"a.latitude IS NOT NULL AND a.longitude IS NOT NULL",
		"a.geo_precision = ANY(?) AND a.geo_source = ANY(?)",
		"a.workspace_id::text AS workspace_id",
		"EXISTS (SELECT 1 FROM leads l WHERE l.id = a.lead_id AND l.deleted_at IS NULL)",
		"EXISTS (SELECT 1 FROM workspaces w WHERE w.id = a.workspace_id AND w.deleted_at IS NULL)",
		"ORDER BY a.id LIMIT ?",
	} {
		if !strings.Contains(locatedAfterSQL, want) {
			t.Fatalf("located after SQL misses %q: %s", want, locatedAfterSQL)
		}
	}
}

func TestLocatedAfterPagesByIDWithTheRefiningRules(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	precisions := pq.StringArray{}
	for _, p := range georef.RefiningPrecisions() {
		precisions = append(precisions, string(p))
	}
	sources := pq.StringArray{}
	for _, s := range georef.RefiningSources() {
		sources = append(sources, string(s))
	}
	mock.ExpectQuery(exact(locatedAfterSQL)).WithArgs(firstAddressID, precisions, sources, 500).
		WillReturnRows(sqlmock.NewRows([]string{"id", "workspace_id", "zip_code", "city_code", "city", "state", "district", "latitude", "longitude", "geo_precision", "geo_source"}).
			AddRow("a-1", "ws-1", "69301000", nil, "Boa Vista", "RR", "Centro", 2.82, -60.67, "exact", "manual"))
	got, err := NewRefinements(db).LocatedAfter(context.Background(), "", 500)
	if err != nil || len(got) != 1 {
		t.Fatalf("LocatedAfter() = %+v, %v", got, err)
	}
	want := georef.LocatedAddress{
		ID: "a-1", WorkspaceID: "ws-1", Postal: address.Postal{ZipCode: "69301000", City: "Boa Vista", State: "RR", District: "Centro"},
		Point: geo.Point{Lat: 2.82, Lng: -60.67}, Precision: geo.PrecisionExact, Source: geo.SourceManual,
	}
	if got[0] != want {
		t.Fatalf("LocatedAfter() = %+v, want %+v", got[0], want)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestLocatedAfterRefusesABadPageSize(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	if _, err := NewRefinements(db).LocatedAfter(context.Background(), "", 0); err == nil {
		t.Fatal("LocatedAfter() accepted a zero page")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRefineDistrictsWritesLeadPointsInBatches(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	points := make([]georef.DistrictPoint, upsertBatch+1)
	for i := range points {
		points[i] = georef.DistrictPoint{CityCode: "1400100", DistrictKey: "centro", Name: "Centro", SampleCount: 5}
	}
	builtAt := time.Date(2026, 10, 9, 6, 0, 0, 0, time.UTC)
	rest := make([]driver.Value, 11)
	for i := range rest {
		rest[i] = sqlmock.AnyArg()
	}
	head := []driver.Value{georef.SourceLeads, builtAt}
	args := append(append(head, rest...), pq.StringArray{georef.SourceLeads})
	mock.ExpectExec(exact(upsertDistrictsSQL)).WithArgs(args...).WillReturnResult(sqlmock.NewResult(0, upsertBatch))
	mock.ExpectExec(exact(upsertDistrictsSQL)).WithArgs(args...).WillReturnResult(sqlmock.NewResult(0, 1))
	if err := NewRefinements(db).RefineDistricts(context.Background(), points, builtAt); err != nil {
		t.Fatal(err)
	}
	mock.ExpectExec(exact(dropLeadDistrictsSQL)).WithArgs(builtAt).WillReturnResult(sqlmock.NewResult(0, 3))
	dropped, err := NewRefinements(db).DropLeadDistrictsBefore(context.Background(), builtAt)
	if err != nil || dropped != 3 {
		t.Fatalf("DropLeadDistrictsBefore() = %d, %v", dropped, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRefineDistrictsRefusesAZeroRunTime(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	if err := NewRefinements(db).RefineDistricts(context.Background(), nil, time.Time{}); err == nil {
		t.Fatal("RefineDistricts() accepted a zero run time")
	}
	if _, err := NewRefinements(db).DropLeadDistrictsBefore(context.Background(), time.Time{}); err == nil {
		t.Fatal("DropLeadDistrictsBefore() accepted a zero run time, which would keep every stale point or drop fresh ones")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

type stringState struct {
	domainCache.SharedState
	values map[string]string
	ttls   map[string]time.Duration
	err    error
}

func (s *stringState) GetString(key string) (string, error) { return s.values[key], s.err }

func (s *stringState) SetString(key, value string, ttl time.Duration) error {
	if s.err != nil {
		return s.err
	}
	s.values[key], s.ttls[key] = value, ttl
	return nil
}

func TestRefineRunsRememberTheLastNight(t *testing.T) {
	state := &stringState{values: map[string]string{}, ttls: map[string]time.Duration{}}
	runs, err := NewRefineRuns(state)
	if err != nil {
		t.Fatal(err)
	}
	last, err := runs.LastRefine(context.Background())
	if err != nil || !last.IsZero() {
		t.Fatalf("LastRefine() on a fresh store = %v, %v, want never", last, err)
	}
	at := time.Date(2026, 10, 9, 6, 15, 0, 0, time.UTC)
	if err := runs.MarkRefine(context.Background(), at); err != nil {
		t.Fatal(err)
	}
	if state.ttls[refineRunKey] < 48*time.Hour {
		t.Fatalf("the run is kept %s, want at least two days", state.ttls[refineRunKey])
	}
	last, err = runs.LastRefine(context.Background())
	if err != nil || !last.Equal(at) {
		t.Fatalf("LastRefine() = %v, %v, want %v", last, err, at)
	}
}

func TestRefineRunsFailClosed(t *testing.T) {
	if _, err := NewRefineRuns(nil); err == nil {
		t.Fatal("NewRefineRuns(nil) accepted no shared state")
	}
	boom := errors.New("boom")
	state := &stringState{values: map[string]string{refineRunKey: "not a time"}, ttls: map[string]time.Duration{}}
	runs, _ := NewRefineRuns(state)
	if _, err := runs.LastRefine(context.Background()); err == nil {
		t.Fatal("LastRefine() read an unreadable mark as never run")
	}
	state.err = boom
	if _, err := runs.LastRefine(context.Background()); !errors.Is(err, boom) {
		t.Fatalf("LastRefine() = %v, want the shared state error", err)
	}
}

func TestCityCodesReadsOnlyTheCEPsAndCityNames(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	postals := []address.Postal{
		{ZipCode: "69301000", District: "Centro", City: "Boa Vista", State: "RR"},
		{District: "Centro", City: "Contagem", State: "MG"},
	}
	mock.ExpectQuery(exact(cepPointsSQL)).WithArgs(sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"zip_code", "latitude", "longitude", "spread_m", "address_count", "city_code"}).
			AddRow("69301000", 2.82, -60.67, 2000.0, 340, "1400100"))
	mock.ExpectQuery(exact(cityCodesByNameSQL)).WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"city_code", "state", "name_key"}).AddRow("3118601", "MG", "contagem"))
	idx, err := NewReference(db).CityCodes(context.Background(), postals)
	if err != nil {
		t.Fatal(err)
	}
	if idx.CityCodeOf(postals[0]) != "1400100" || idx.CityCodeOf(postals[1]) != "3118601" {
		t.Fatalf("CityCodes() = %+v, want Boa Vista through its CEP and Contagem through its name", idx)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
