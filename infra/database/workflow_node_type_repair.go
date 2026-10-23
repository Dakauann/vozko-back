package database

import (
	"fmt"
	"log/slog"
	"strings"

	"gorm.io/gorm"

	"vozko/domain/workflow"
	"vozko/infra/database/sqlexpr"
)

const (
	legacyRunStepRenameBatch = 5000
	LegacyRunStepTypeIndex   = "idx_wfrl_legacy_node_type"
)

const validIndexSQL = `SELECT EXISTS (SELECT 1 FROM pg_index WHERE indexrelid = to_regclass(?) AND indisvalid AND indisready)`

func canonicalizeLegacyNodeTypes(tx *gorm.DB) error {
	for _, table := range []string{"workflows", "workflow_versions"} {
		if !tx.Migrator().HasTable(table) {
			return nil
		}
	}
	return renameLegacyNodeTypes(tx, workflowGraphCanonicalBatch)
}

func renameLegacyNodeTypes(tx *gorm.DB, graphBatch int) error {
	return canonicalizeGraphsMatching(tx, workflow.LegacyNodeTypeGraphPath(), graphBatch)
}

func runIndexedRepairs(db *gorm.DB) {
	if err := renameLegacyRunStepTypesOnceIndexed(db, legacyRunStepRenameBatch); err != nil {
		slog.Error("workflow.legacy_run_step_types.failed", slog.String("error", err.Error()))
	}
}

func renameLegacyRunStepTypesOnceIndexed(db *gorm.DB, batch int) error {
	var indexed bool
	if err := db.Raw(validIndexSQL, LegacyRunStepTypeIndex).Scan(&indexed).Error; err != nil {
		return fmt.Errorf("check %s: %w", LegacyRunStepTypeIndex, err)
	}
	if !indexed {
		slog.Warn("workflow.legacy_run_step_types.waiting_for_index", slog.String("index", LegacyRunStepTypeIndex))
		return nil
	}
	return renameLegacyRunStepTypes(db, batch)
}

func renameLegacyRunStepTypes(tx *gorm.DB, batch int) error {
	rename := renameLegacyRunStepTypesSQL()
	for {
		result := tx.Exec(rename, batch)
		if result.Error != nil {
			return fmt.Errorf("workflow_run_logs: %w", result.Error)
		}
		if result.RowsAffected < int64(batch) {
			return nil
		}
	}
}

func legacyRunStepTypePredicate() string {
	return "node_type IN (" + sqlexpr.LiteralList(legacyNodeTypeNames()) + ")"
}

func renameLegacyRunStepTypesSQL() string {
	var cases strings.Builder
	for _, legacy := range workflow.LegacyNodeTypes() {
		fmt.Fprintf(&cases, " WHEN %s THEN %s", sqlexpr.Literal(string(legacy)), sqlexpr.Literal(string(legacy.Canonical())))
	}
	return "UPDATE workflow_run_logs SET node_type = CASE node_type" + cases.String() + " END WHERE id IN (SELECT id FROM workflow_run_logs WHERE " + legacyRunStepTypePredicate() + " LIMIT ?)"
}

func legacyRunStepTypeIndexSQL() string {
	return "CREATE INDEX CONCURRENTLY IF NOT EXISTS " + LegacyRunStepTypeIndex + " ON workflow_run_logs (id) WHERE " + legacyRunStepTypePredicate()
}

func legacyNodeTypeNames() []string {
	legacy := workflow.LegacyNodeTypes()
	names := make([]string, len(legacy))
	for i, nodeType := range legacy {
		names[i] = string(nodeType)
	}
	return names
}
