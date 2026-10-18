package database

import (
	"slices"
	"sync"
	"testing"

	gormschema "gorm.io/gorm/schema"

	"vozko/infra/database/schema"
)

func TestRetiredSettingColumnsRepairIsRegistered(t *testing.T) {
	if !slices.Contains(repairNames(), "drop_retired_setting_columns") {
		t.Fatal("the retired setting columns cleanup is not registered in runDataRepairs")
	}
}

func TestRetiredSettingColumnsAreGoneFromTheSchema(t *testing.T) {
	models := map[string]interface{}{
		"whatsapp_campaigns":            &schema.WhatsAppCampaign{},
		"unofficial_whatsapp_campaigns": &schema.UnofficialWhatsAppCampaign{},
		"agents":                        &schema.Agent{},
		"unofficial_whatsapp_instances": &schema.UnofficialWhatsAppInstance{},
		"template_exit_settings":        &schema.TemplateExitSettings{},
	}
	for _, column := range retiredSettingColumns {
		model, ok := models[column.table]
		if !ok {
			t.Fatalf("no schema model for %s", column.table)
		}
		parsed, err := gormschema.Parse(model, &sync.Map{}, gormschema.NamingStrategy{})
		if err != nil {
			t.Fatalf("parse %s: %v", column.table, err)
		}
		if parsed.Table != column.table {
			t.Fatalf("model for %s maps to %s", column.table, parsed.Table)
		}
		if parsed.LookUpField(column.name) != nil {
			t.Fatalf("%s.%s is still in the schema, so AutoMigrate would add it back after the repair drops it", column.table, column.name)
		}
	}
}

func TestDropRetiredSettingColumnsAgainstPostgres(t *testing.T) {
	tx := repairTx(t)
	for _, ddl := range []string{
		`DROP TABLE IF EXISTS whatsapp_campaigns, unofficial_whatsapp_campaigns, agents, unofficial_whatsapp_instances CASCADE`,
		`CREATE TABLE whatsapp_campaigns (id uuid PRIMARY KEY, name text, prefer_audio boolean NOT NULL DEFAULT false)`,
		`CREATE TABLE unofficial_whatsapp_campaigns (id uuid PRIMARY KEY, name text, prefer_audio boolean NOT NULL DEFAULT false)`,
		`CREATE TABLE agents (id uuid PRIMARY KEY, name text, initial_message text NOT NULL, use_initial_message boolean NOT NULL DEFAULT true)`,
		`CREATE TABLE unofficial_whatsapp_instances (id uuid PRIMARY KEY, display_name text, auto_reject_calls boolean NOT NULL DEFAULT false)`,
	} {
		if err := tx.Exec(ddl).Error; err != nil {
			t.Fatalf("fixture table: %v", err)
		}
	}

	if err := dropRetiredSettingColumns(tx); err != nil {
		t.Fatalf("repair: %v", err)
	}
	if err := dropRetiredSettingColumns(tx); err != nil {
		t.Fatalf("a second run must be harmless: %v", err)
	}

	for _, column := range retiredSettingColumns {
		if tx.Migrator().HasColumn(column.table, column.name) {
			t.Fatalf("%s.%s was not dropped", column.table, column.name)
		}
	}
	for table, kept := range map[string]string{
		"whatsapp_campaigns":            "name",
		"unofficial_whatsapp_campaigns": "name",
		"agents":                        "name",
		"unofficial_whatsapp_instances": "display_name",
	} {
		if !tx.Migrator().HasColumn(table, kept) {
			t.Fatalf("%s.%s was dropped too", table, kept)
		}
	}
	if err := tx.Exec(`INSERT INTO agents (id, name) VALUES (gen_random_uuid(), 'Nova')`).Error; err != nil {
		t.Fatalf("an agent without an initial message must save: %v", err)
	}
}

func TestDropRetiredSettingColumnsSkipsMissingTables(t *testing.T) {
	tx := repairTx(t)
	for _, column := range retiredSettingColumns {
		if err := tx.Exec(`DROP TABLE IF EXISTS ` + column.table + ` CASCADE`).Error; err != nil {
			t.Fatalf("clear %s: %v", column.table, err)
		}
	}
	if err := dropRetiredSettingColumns(tx); err != nil {
		t.Fatalf("repair on a database without the tables: %v", err)
	}
}
