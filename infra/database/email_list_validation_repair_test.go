package database

import (
	"sync"
	"testing"

	gormschema "gorm.io/gorm/schema"

	"vozko/infra/database/schema"
)

func TestTheEmailListValidationRepairIsDeclared(t *testing.T) {
	for _, name := range repairNames() {
		if name == "em_drop_list_validation" {
			return
		}
	}
	t.Fatal("the list validation table and column must be dropped at boot")
}

func TestTheEmailListValidationIsGoneFromTheSchema(t *testing.T) {
	for _, model := range schema.EmailCampaignModels() {
		parsed, err := gormschema.Parse(model, &sync.Map{}, gormschema.NamingStrategy{})
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		if parsed.Table == retiredEmailChecksTable {
			t.Fatalf("%s is still migrated, so AutoMigrate would create it again after the repair drops it", retiredEmailChecksTable)
		}
		if parsed.Table == retiredEmailCampaignsTable && parsed.LookUpField(retiredEmailValidationColumn) != nil {
			t.Fatalf("%s.%s is still in the schema", retiredEmailCampaignsTable, retiredEmailValidationColumn)
		}
	}
}

func TestDropEmailListValidationAgainstPostgres(t *testing.T) {
	tx := postgresTx(t)
	for _, ddl := range []string{
		`DROP TABLE IF EXISTS email_address_checks, email_campaigns CASCADE`,
		`CREATE TABLE email_address_checks (id uuid PRIMARY KEY, address varchar(254) NOT NULL)`,
		`CREATE TABLE email_campaigns (id uuid PRIMARY KEY, name varchar(120) NOT NULL, validation jsonb)`,
	} {
		if err := tx.Exec(ddl).Error; err != nil {
			t.Fatalf("fixture: %v", err)
		}
	}
	if err := dropEmailListValidation(tx); err != nil {
		t.Fatalf("repair: %v", err)
	}
	if err := dropEmailListValidation(tx); err != nil {
		t.Fatalf("a second run must be harmless: %v", err)
	}
	if tx.Migrator().HasTable(retiredEmailChecksTable) {
		t.Fatal("the checks table survived")
	}
	if tx.Migrator().HasColumn(retiredEmailCampaignsTable, retiredEmailValidationColumn) {
		t.Fatal("the campaign validation column survived")
	}
	if !tx.Migrator().HasColumn(retiredEmailCampaignsTable, "name") {
		t.Fatal("the repair must only drop the validation column")
	}
}
