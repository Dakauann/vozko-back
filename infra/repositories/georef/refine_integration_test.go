package georef_repository

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"vozko/domain/georef"
	"vozko/infra/database"
	"vozko/infra/database/schema"
	"vozko/infra/repositories/repotest"
	geocoding_usecase "vozko/usecases/geocoding"
)

type memoryRuns struct{ last time.Time }

func (m *memoryRuns) LastRefine(context.Context) (time.Time, error) { return m.last, nil }

func (m *memoryRuns) MarkRefine(_ context.Context, at time.Time) error {
	m.last = at
	return nil
}

func refineDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := repotest.IsolatedDB(t, "georef_refine")
	db.Config.DisableForeignKeyConstraintWhenMigrating = true
	if err := db.AutoMigrate(&schema.Workspace{}, &schema.Lead{}, &schema.LeadAddress{}, &schema.GeoCEPPoint{}, &schema.GeoCity{}, &schema.GeoDistrictPoint{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func TestNightlyRefinementFillsBairrosFromLocatedLeadsAgainstPostgres(t *testing.T) {
	db := refineDB(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 9, 6, 30, 0, 0, time.UTC)
	workspace := func(deleted bool) string {
		id := uuid.NewString()
		var deletedAt any
		if deleted {
			deletedAt = now
		}
		if err := db.Exec("INSERT INTO workspaces (id, owner_id, name, created_at, updated_at, deleted_at) VALUES (?, ?, 'Workspace', ?, ?, ?)", id, uuid.NewString(), now, now, deletedAt).Error; err != nil {
			t.Fatal(err)
		}
		return id
	}
	school, clinic, closed := workspace(false), workspace(false), workspace(true)
	if err := db.Exec("INSERT INTO geo_cities (city_code, name, name_key, state, latitude, longitude, built_at) VALUES ('1400100', 'Boa Vista', 'boa vista', 'RR', 2.82, -60.67, ?)", now).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO geo_district_points (city_code, district_key, name, latitude, longitude, spread_m, sample_count, source, built_at) VALUES ('1400100', 'centro', 'CENTRO', 2.80, -60.60, 400, 5000, ?, ?)", georef.SourceCNEFE, now.AddDate(0, -1, 0)).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO geo_district_points (city_code, district_key, name, latitude, longitude, spread_m, sample_count, source, built_at) VALUES ('1400100', 'bairro antigo', 'Bairro Antigo', 2.90, -60.90, 100, 6, ?, ?)", georef.SourceLeads, now.AddDate(0, 0, -1)).Error; err != nil {
		t.Fatal(err)
	}
	seed := func(ws, district string, lat float64, precision, source string, deleted bool) {
		leadID := uuid.NewString()
		var deletedAt any
		if deleted {
			deletedAt = now
		}
		if err := db.Exec("INSERT INTO leads (id, workspace_id, name, created_at, updated_at, deleted_at) VALUES (?, ?, 'Lead', ?, ?, ?)", leadID, ws, now, now, deletedAt).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Exec(`INSERT INTO lead_addresses (id, workspace_id, lead_id, label, is_primary, position, city, state, district, district_key, latitude, longitude, geo_precision, geo_source, geo_status, fingerprint, created_at, updated_at)
			VALUES (?, ?, ?, 'home', true, 0, 'Boa Vista', 'RR', ?, lower(?), ?, -60.70, ?, ?, 'located', 'f', ?, ?)`,
			uuid.NewString(), ws, leadID, district, district, lat, precision, source, now, now).Error; err != nil {
			t.Fatal(err)
		}
	}
	for i := range 3 {
		seed(school, "Pintolândia", 2.800+float64(i)*0.001, "exact", "manual", false)
		seed(clinic, "Pintolândia", 2.803+float64(i)*0.001, "exact", "lead_pin", false)
	}
	for i := range 5 {
		seed(school, "Centro", 2.81+float64(i)*0.001, "address", "provider", false)
		seed(clinic, "Centro", 2.82+float64(i)*0.001, "address", "provider", false)
	}
	for i := range 10 {
		seed(closed, "Pintolândia", 2.60+float64(i)*0.001, "exact", "manual", false)
	}
	seed(school, "Pintolândia", 2.70, "exact", "manual", true)
	seed(school, "Pintolândia", 2.71, "street", "reference", false)
	seed(clinic, "Pintolândia", 2.72, "district", "manual", false)

	refinements := NewRefinements(db)
	refine, err := geocoding_usecase.NewDistrictRefine(geocoding_usecase.DistrictRefineDeps{
		Addresses: refinements, Cities: NewReference(db), Districts: refinements, Runs: &memoryRuns{},
		Now: func() time.Time { return now }, BatchSize: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := refine.Run(ctx); err != nil {
		t.Fatal(err)
	}

	var rows []struct {
		DistrictKey string
		Name        string
		Latitude    float64
		SampleCount int64
		Source      string
	}
	if err := db.Raw("SELECT district_key, name, latitude, sample_count, source FROM geo_district_points ORDER BY district_key").Scan(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("points = %+v, want the census Centro and the new Pintolândia, with the stale lead point dropped", rows)
	}
	if rows[0].DistrictKey != "centro" || rows[0].Source != georef.SourceCNEFE || rows[0].Latitude != 2.80 || rows[0].SampleCount != 5000 {
		t.Fatalf("centro = %+v, want the census point untouched", rows[0])
	}
	p := rows[1]
	if p.DistrictKey != "pintolandia" || p.Source != georef.SourceLeads || p.SampleCount != 6 || p.Name != "Pintolândia" || p.Latitude < 2.8024 || p.Latitude > 2.8026 {
		t.Fatalf("pintolandia = %+v, want six live precise positions of two workspaces around 2.8025, ignoring deleted leads, a closed workspace and reference or bairro positions", p)
	}

	if err := db.Exec("UPDATE geo_district_points SET source = ? WHERE district_key = 'centro'", georef.SourceLeads).Error; err != nil {
		t.Fatal(err)
	}
	later := now.AddDate(0, 0, 1)
	again, _ := geocoding_usecase.NewDistrictRefine(geocoding_usecase.DistrictRefineDeps{
		Addresses: refinements, Cities: NewReference(db), Districts: refinements, Runs: &memoryRuns{last: now},
		Now: func() time.Time { return later },
	})
	if err := again.Run(ctx); err != nil {
		t.Fatal(err)
	}
	var centro struct {
		SampleCount int64
		BuiltAt     time.Time
	}
	if err := db.Raw("SELECT sample_count, built_at FROM geo_district_points WHERE district_key = 'centro'").Scan(&centro).Error; err != nil {
		t.Fatal(err)
	}
	if centro.SampleCount != 10 || !centro.BuiltAt.Equal(later) {
		t.Fatalf("centro = %+v, want a point refined from leads refreshed by the next night", centro)
	}
}

func TestLocatedAfterReadsLocatedAddressesThroughTheirIDIndexAgainstPostgres(t *testing.T) {
	db := refineDB(t)
	indexSQL, ok := database.ConcurrentIndexSQL(database.LeadAddressLocatedIndex)
	if !ok {
		t.Fatalf("%s is not built", database.LeadAddressLocatedIndex)
	}
	for _, statement := range []string{
		indexSQL,
		"INSERT INTO workspaces (id, owner_id, name, created_at, updated_at)" +
			" SELECT ('00000000-0000-0000-0000-' || lpad(g::text, 12, '0'))::uuid, gen_random_uuid(), 'Workspace', now(), now() FROM generate_series(0, 39) g",
		"INSERT INTO leads (id, workspace_id, name, created_at, updated_at)" +
			" SELECT gen_random_uuid(), ('00000000-0000-0000-0000-' || lpad((g % 40)::text, 12, '0'))::uuid, 'Lead', now(), now() FROM generate_series(1, 60000) g",
		"INSERT INTO lead_addresses (id, workspace_id, lead_id, label, is_primary, position, district, district_key, latitude, longitude, geo_precision, geo_source, geo_status, fingerprint, created_at, updated_at)" +
			" SELECT gen_random_uuid(), l.workspace_id, l.id, 'home', true, 0, 'Centro', 'centro'," +
			" CASE WHEN random() < 0.03 THEN -23.5 END, CASE WHEN random() < 0.03 THEN -46.6 END, 'exact', 'manual', 'located', md5(l.id::text)::varchar(32), now(), now()" +
			" FROM leads l",
		"ANALYZE workspaces",
		"ANALYZE leads",
		"ANALYZE lead_addresses",
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	var plan []string
	if err := db.Raw("EXPLAIN "+locatedAfterSQL, firstAddressID, refiningPrecisions(), refiningSources(), georef.RefineBatch).Scan(&plan).Error; err != nil {
		t.Fatalf("explain: %v", err)
	}
	text := strings.Join(plan, "\n")
	if !strings.Contains(text, database.LeadAddressLocatedIndex) || strings.Contains(text, "Seq Scan on lead_addresses") {
		t.Fatalf("a refinement page must read located addresses through %s, never a full scan:\n%s", database.LeadAddressLocatedIndex, text)
	}
}
