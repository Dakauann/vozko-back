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
		{"em_drop_list_validation", dropEmailListValidation},
		{"em_drop_run_charge_columns", dropEmailRunChargeColumns},
		{"cm_backfill_awaiting_reply", backfillAwaitingReply},
		{"cm_mark_note_images", markNoteImages},
		{"cm_mark_email_bodies", markEmailBodies},
		{"wf_publish_running_workflows", publishRunningWorkflows},
		{"wf_canonical_structured_configs", canonicalizeWorkflowGraphs},
		{"wf_canonical_node_types", canonicalizeLegacyNodeTypes},
		{"inv_external_id_per_provider", repairInvoiceExternalIDs},
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
