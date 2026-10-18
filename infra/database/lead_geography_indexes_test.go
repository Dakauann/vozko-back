package database

import (
	"testing"

	"vozko/infra/database/schema"
	"vozko/infra/repositories/repotest"
)

func TestTheWindowIndexBuildsOnLeadAddressesAgainstPostgres(t *testing.T) {
	db := repotest.IsolatedDB(t, "lead_window_index")
	db.Config.DisableForeignKeyConstraintWhenMigrating = true
	if err := db.AutoMigrate(&schema.LeadAddress{}, &schema.LeadArea{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := createIndexConcurrently(db, declaredConcurrentIndex(t, LeadAddressWindowIndex)); err != nil {
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
