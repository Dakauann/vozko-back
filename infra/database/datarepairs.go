package database

import (
	"fmt"

	"gorm.io/gorm"
)

type dataRepair struct {
	name string
	run  func(*gorm.DB) error
}

func repairNames() []string {
	all := dataRepairs()
	names := make([]string, 0, len(all))
	for _, r := range all {
		names = append(names, r.name)
	}
	return names
}

func dataRepairs() []dataRepair {
	return []dataRepair{
		{"wc_consume_used_schedules", consumeUsedSchedules},
		{"drop_retired_setting_columns", dropRetiredSettingColumns},
		{"lx_drop_workspace_exit_settings", dropWorkspaceExitSettings},
	}
}

func runDataRepairs(tx *gorm.DB) error {
	for _, r := range dataRepairs() {
		if err := r.run(tx); err != nil {
			return fmt.Errorf("data repair %s: %w", r.name, err)
		}
	}
	return nil
}

func dropWorkspaceExitSettings(tx *gorm.DB) error {
	return tx.Exec("DROP TABLE IF EXISTS workspace_exit_settings").Error
}
