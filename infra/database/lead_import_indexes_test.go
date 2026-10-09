package database

import (
	"strings"
	"testing"
)

func TestTheImportIndexCountsAnImportsAddressesFromTheIndexAlone(t *testing.T) {
	sql := strings.Join(strings.Fields(namedIndex(t, "idx_lead_addresses_import").sql), " ")
	want := "CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_lead_addresses_import ON lead_addresses (import_id) INCLUDE (workspace_id, lead_id, latitude, geo_status, geo_precision) WHERE import_id IS NOT NULL"
	if sql != want {
		t.Fatalf("import index = %q, want %q", sql, want)
	}
}
