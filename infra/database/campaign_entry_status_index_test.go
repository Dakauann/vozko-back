package database

import (
	"strings"
	"testing"

	"vozko/infra/database/schema"
	"vozko/infra/repositories/repotest"
)

const legacyCampaignEntryStatusIndex = "idx_wce_campaign_status_del"

func TestTheLiveCampaignStatusIndexCarriesTheLeadSoLeadFacetsReadOnlyTheIndex(t *testing.T) {
	idx := namedIndex(t, CampaignEntryLiveStatusIndex)
	sql := strings.Join(strings.Fields(idx.sql), " ")
	want := "CREATE INDEX CONCURRENTLY IF NOT EXISTS " + CampaignEntryLiveStatusIndex +
		" ON whatsapp_campaign_entries (campaign_id, status) INCLUDE (lead_id) WHERE deleted_at IS NULL"
	if sql != want {
		t.Fatalf("live status index = %q, want %q", sql, want)
	}
	if idx.replaces != legacyCampaignEntryStatusIndex {
		t.Fatalf("live status index replaces %q, want %q", idx.replaces, legacyCampaignEntryStatusIndex)
	}
}

func TestTheLiveCampaignStatusIndexBuildsAndDropsTheOneItReplacesAgainstPostgres(t *testing.T) {
	db := repotest.IsolatedDB(t, "campaign_entry_status_index")
	db.Config.DisableForeignKeyConstraintWhenMigrating = true
	if err := db.AutoMigrate(&schema.WhatsAppCampaignEntry{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	legacy := `CREATE INDEX ` + legacyCampaignEntryStatusIndex + ` ON whatsapp_campaign_entries (campaign_id, status) WHERE deleted_at IS NULL`
	if err := db.Exec(legacy).Error; err != nil {
		t.Fatalf("legacy index: %v", err)
	}
	if err := createIndexConcurrently(db, namedIndex(t, CampaignEntryLiveStatusIndex)); err != nil {
		t.Fatalf("%s: %v", CampaignEntryLiveStatusIndex, err)
	}
	present := func(name string) (exists, valid bool) {
		row := struct{ Exists, Valid bool }{}
		if err := db.Raw(`SELECT i.indexrelid IS NOT NULL AS exists, COALESCE(i.indisvalid, false) AS valid
			FROM (SELECT to_regclass(?::text) AS oid) r LEFT JOIN pg_index i ON i.indexrelid = r.oid`, name).Scan(&row).Error; err != nil {
			t.Fatalf("index lookup %s: %v", name, err)
		}
		return row.Exists, row.Valid
	}
	if exists, valid := present(CampaignEntryLiveStatusIndex); !exists || !valid {
		t.Fatalf("%s exists %v valid %v, want a valid index", CampaignEntryLiveStatusIndex, exists, valid)
	}
	if exists, _ := present(legacyCampaignEntryStatusIndex); exists {
		t.Fatalf("%s is still there after its replacement became valid", legacyCampaignEntryStatusIndex)
	}
}

func TestTheReplacedCampaignStatusIndexIsNoLongerRebuiltAtBoot(t *testing.T) {
	if sql, ok := PerformanceIndexSQL(legacyCampaignEntryStatusIndex); ok {
		t.Fatalf("%s is still declared and would be rebuilt after its replacement drops it: %s", legacyCampaignEntryStatusIndex, sql)
	}
}
