package database

import (
	"reflect"
	"strings"
	"testing"

	"vozko/infra/database/schema"
	"vozko/infra/repositories/repotest"
)

func TestTheHoldersOfANumberAreReadInLeadOrderThroughOneIndex(t *testing.T) {
	var found *concurrentIndex
	for _, idx := range concurrentIndexes() {
		if idx.name == LeadPhoneHoldersIndex {
			found = &idx
		}
	}
	if found == nil {
		t.Fatalf("%s is not built", LeadPhoneHoldersIndex)
	}
	if sql := strings.Join(strings.Fields(found.sql), " "); !strings.Contains(sql, "ON lead_phones (workspace_id, number, lead_id)") {
		t.Fatalf("%s = %q, want the lead id after the number", LeadPhoneHoldersIndex, sql)
	}
	if found.replaces != "idx_lead_phones_workspace_number" {
		t.Fatalf("%s replaces %q, want the (workspace_id, number) index it covers", LeadPhoneHoldersIndex, found.replaces)
	}
}

func TestTheReplacedPhoneIndexIsNoLongerDeclaredOnTheModel(t *testing.T) {
	model := reflect.TypeOf(schema.LeadPhone{})
	for i := 0; i < model.NumField(); i++ {
		if tag := model.Field(i).Tag.Get("gorm"); strings.Contains(tag, "idx_lead_phones_workspace_number") {
			t.Fatalf("%s still declares the replaced index: AutoMigrate would build it again on every boot", model.Field(i).Name)
		}
	}
}

func TestTheHoldersIndexReplacesThePhoneIndexAgainstPostgres(t *testing.T) {
	db := repotest.IsolatedDB(t, "lead_phone_holders")
	db.Config.DisableForeignKeyConstraintWhenMigrating = true
	if err := db.AutoMigrate(&schema.Lead{}, &schema.LeadPhone{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := db.Exec("CREATE INDEX idx_lead_phones_workspace_number ON lead_phones (workspace_id, number)").Error; err != nil {
		t.Fatalf("the index production has today: %v", err)
	}
	for _, idx := range concurrentIndexes() {
		if idx.name == LeadPhoneHoldersIndex {
			if err := createIndexConcurrently(db, idx); err != nil {
				t.Fatalf("%s: %v", idx.name, err)
			}
		}
	}
	present := func(name string) bool {
		var found bool
		if err := db.Raw("SELECT to_regclass(?::text) IS NOT NULL", name).Scan(&found).Error; err != nil {
			t.Fatalf("lookup %s: %v", name, err)
		}
		return found
	}
	if !present(LeadPhoneHoldersIndex) || present("idx_lead_phones_workspace_number") {
		t.Fatal("the holders index must be built and the index it covers dropped")
	}
	if err := db.AutoMigrate(&schema.LeadPhone{}); err != nil || present("idx_lead_phones_workspace_number") {
		t.Fatalf("a later boot must not build the replaced index again: %v", err)
	}
}
