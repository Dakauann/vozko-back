package database

import (
	"sync"
	"testing"

	gormschema "gorm.io/gorm/schema"

	"vozko/infra/database/schema"
)

func TestTheEmailRunChargeRepairIsDeclared(t *testing.T) {
	for _, name := range repairNames() {
		if name == "em_drop_run_charge_columns" {
			return
		}
	}
	t.Fatal("the up-front run charge columns must be dropped at boot")
}

func TestTheEmailRunChargeColumnsAreGoneFromTheSchema(t *testing.T) {
	for _, model := range schema.EmailCampaignModels() {
		parsed, err := gormschema.Parse(model, &sync.Map{}, gormschema.NamingStrategy{})
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		if parsed.Table != retiredEmailCampaignsTable {
			continue
		}
		for _, column := range retiredEmailRunChargeColumns {
			if parsed.LookUpField(column) != nil {
				t.Fatalf("%s.%s is still in the schema, so AutoMigrate would add it back", retiredEmailCampaignsTable, column)
			}
		}
	}
}

func TestDropEmailRunChargeColumnsAgainstPostgres(t *testing.T) {
	tx := postgresTx(t)
	for _, ddl := range []string{
		`DROP TABLE IF EXISTS email_campaigns CASCADE`,
		`CREATE TABLE email_campaigns (id uuid PRIMARY KEY, paid_units bigint NOT NULL DEFAULT 0, charged_units bigint NOT NULL DEFAULT 0,
			unit_price_micros bigint NOT NULL DEFAULT 0, unit_cost_micros bigint NOT NULL DEFAULT 0, charged_total_micros bigint NOT NULL DEFAULT 0)`,
	} {
		if err := tx.Exec(ddl).Error; err != nil {
			t.Fatalf("fixture: %v", err)
		}
	}
	if err := dropEmailRunChargeColumns(tx); err != nil {
		t.Fatalf("repair: %v", err)
	}
	if err := dropEmailRunChargeColumns(tx); err != nil {
		t.Fatalf("a second run must be harmless: %v", err)
	}
	for _, column := range retiredEmailRunChargeColumns {
		if tx.Migrator().HasColumn(retiredEmailCampaignsTable, column) {
			t.Fatalf("%s survived", column)
		}
	}
	if !tx.Migrator().HasColumn(retiredEmailCampaignsTable, "paid_units") {
		t.Fatal("the repair must only drop the retired columns")
	}
}
