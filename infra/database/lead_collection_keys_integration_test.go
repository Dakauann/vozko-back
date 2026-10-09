package database

import (
	"testing"

	"gorm.io/gorm"

	"vozko/infra/database/schema"
	"vozko/infra/repositories/repotest"
)

func TestCompositeLeadKeysWaitForTheLeadIndexThenStayValidAgainstPostgres(t *testing.T) {
	db := repotest.IsolatedDB(t, "lead_keys")
	db.Config.DisableForeignKeyConstraintWhenMigrating = true
	if err := db.AutoMigrate(&schema.Lead{}, &schema.LeadPhone{}, &schema.LeadAddress{}, &schema.LeadRelation{}, &schema.CallList{}, &schema.CallListItem{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		return CreateLeadCollectionConstraints(tx)
	}); err != nil {
		t.Fatalf("constraints: %v", err)
	}

	if err := EnsureCompositeLeadKeys(db); err == nil {
		t.Fatal("without the (workspace_id, id) index the keys cannot exist yet and the step must say so")
	}

	for _, idx := range concurrentIndexes() {
		if idx.name == "ux_leads_workspace_id_id" {
			if err := createIndexConcurrently(db, idx); err != nil {
				t.Fatalf("index: %v", err)
			}
		}
	}
	for i := 0; i < 2; i++ {
		if err := EnsureCompositeLeadKeys(db); err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
	}
	var valid int64
	db.Raw("SELECT COUNT(*) FROM pg_constraint c JOIN pg_namespace n ON n.oid = c.connamespace WHERE n.nspname = current_schema() AND c.conname LIKE ? AND c.convalidated", "fk_lead_%_workspace_%").Scan(&valid)
	if valid != int64(len(compositeLeadKeys)) {
		t.Fatalf("valid composite keys = %d, want %d", valid, len(compositeLeadKeys))
	}
}
