package database

import (
	"strings"
	"testing"

	"vozko/infra/database/schema"
	"vozko/infra/repositories/repotest"
)

func namedIndex(t *testing.T, name string) concurrentIndex {
	t.Helper()
	for _, idx := range concurrentIndexes() {
		if idx.name == name {
			return idx
		}
	}
	t.Fatalf("%s is not in the concurrent index list", name)
	return concurrentIndex{}
}

func TestTheWindowIndexServesMapWindowsWithRealEstimates(t *testing.T) {
	sql := strings.Join(strings.Fields(namedIndex(t, LeadAddressWindowIndex).sql), " ")
	want := "CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_lead_addresses_window ON lead_addresses (workspace_id, latitude, longitude) INCLUDE (lead_id, geo_precision) WHERE latitude IS NOT NULL AND is_primary"
	if sql != want {
		t.Fatalf("window index = %q, want %q", sql, want)
	}
}

func TestTheLiveLeadsIndexLetsAMapTileReadItsMembershipFromTheIndexAlone(t *testing.T) {
	sql := strings.Join(strings.Fields(namedIndex(t, LeadLiveIndex).sql), " ")
	want := "CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_leads_workspace_live ON leads (workspace_id, id) WHERE deleted_at IS NULL"
	if sql != want {
		t.Fatalf("live leads index = %q, want %q", sql, want)
	}
}

func TestAreasNeedNoGiSTBecauseTheirRingTestOnlyRefinesTheWindowIndex(t *testing.T) {
	for _, idx := range concurrentIndexes() {
		if idx.name == "idx_lead_addresses_position" || strings.Contains(strings.ToLower(idx.sql), "using gist") {
			t.Fatalf("%s builds a GiST that no area query reads", idx.name)
		}
	}
}

func TestTheWindowIndexBuildsOnLeadAddressesAgainstPostgres(t *testing.T) {
	db := repotest.IsolatedDB(t, "lead_window_index")
	db.Config.DisableForeignKeyConstraintWhenMigrating = true
	if err := db.AutoMigrate(&schema.LeadAddress{}, &schema.LeadArea{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := createIndexConcurrently(db, namedIndex(t, LeadAddressWindowIndex)); err != nil {
		t.Fatalf("%s: %v", LeadAddressWindowIndex, err)
	}
	var valid bool
	if err := db.Raw(`SELECT COALESCE((SELECT i.indisvalid FROM pg_index i WHERE i.indexrelid = to_regclass(?::text)), false)`, LeadAddressWindowIndex).Scan(&valid).Error; err != nil || !valid {
		t.Fatalf("%s is not a valid index: %v", LeadAddressWindowIndex, err)
	}
	var ringType string
	if err := db.Raw(`SELECT data_type FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = 'lead_areas' AND column_name = 'ring'`).Scan(&ringType).Error; err != nil || ringType != "polygon" {
		t.Fatalf("lead_areas.ring = %q, %v, want a polygon column", ringType, err)
	}
}
