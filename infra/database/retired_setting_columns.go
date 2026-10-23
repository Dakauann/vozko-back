package database

import (
	"fmt"

	"gorm.io/gorm"
)

type retiredColumn struct {
	table string
	name  string
}

var retiredSettingColumns = []retiredColumn{
	{"whatsapp_campaigns", "prefer_audio"},
	{"unofficial_whatsapp_campaigns", "prefer_audio"},
	{"agents", "initial_message"},
	{"agents", "use_initial_message"},
	{"unofficial_whatsapp_instances", "auto_reject_calls"},
	{"template_exit_settings", "exit_words"},
	{"template_exit_settings", "exit_risk_acknowledged_by"},
	{"template_exit_settings", "exit_risk_acknowledged_at"},
	{"email_sending_domains", "warmup_started_at"},
}

func dropRetiredSettingColumns(tx *gorm.DB) error {
	for _, column := range retiredSettingColumns {
		if !tx.Migrator().HasTable(column.table) || !tx.Migrator().HasColumn(column.table, column.name) {
			continue
		}
		if err := tx.Exec(fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s", column.table, column.name)).Error; err != nil {
			return fmt.Errorf("%s.%s: %w", column.table, column.name, err)
		}
	}
	return nil
}
