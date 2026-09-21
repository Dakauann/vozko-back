package database

import (
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

var audienceTableRenames = [][2]string{
	{"comment_analyses", "audience_analyses"},
	{"comment_analysis_settings", "audience_settings"},
	{"comment_analysis_container_settings", "audience_container_settings"},
	{"comment_analysis_authors", "audience_authors"},
	{"comment_analysis_rollups", "audience_rollups"},
	{"comment_analysis_batches", "audience_batches"},
	{"comment_analysis_backfills", "audience_backfills"},
	{"comment_analysis_alert_rules", "audience_alert_rules"},
}

var audienceColumnRenames = []struct{ table, from, to string }{
	{"audience_analyses", "source_comment_id", "subject_id"},
	{"audience_analyses", "parent_comment_id", "parent_subject_id"},
	{"audience_analyses", "commented_at", "occurred_at"},
}

const (
	analysisGrantSelectSQL = `
		SELECT p.member_id, p.action
		  FROM workspace_member_permissions p
		 WHERE p.resource = 'analysis'
		   AND NOT EXISTS (
		       SELECT 1 FROM workspace_member_permissions q
		        WHERE q.member_id = p.member_id
		          AND q.resource  = 'audience'
		          AND q.action    = p.action
		   )`

	analysisGrantInsertSQL = `
		INSERT INTO workspace_member_permissions (id, member_id, resource, action, created_at)
		VALUES (?, ?, 'audience', ?, ?)`
)

var audienceValueRenames = []struct{ table, column string }{
	{"workspace_member_permissions", "resource"},
	{"balance_transactions", "service_type"},
	{"pricing_items", "service"},
	{"pricing_audit_log", "service"},
}

func renameCommentAnalysisToAudience(tx *gorm.DB) error {
	if err := foldAnalysisPermissionIntoAudience(tx); err != nil {
		return fmt.Errorf("folding the analysis permission into audience: %w", err)
	}

	pending, err := tableExists(tx, "comment_analyses")
	if err != nil {
		return err
	}
	if !pending {
		return nil
	}

	for _, v := range audienceValueRenames {
		if err := renameStoredValue(tx, v.table, v.column); err != nil {
			return fmt.Errorf("renaming %s.%s values: %w", v.table, v.column, err)
		}
	}
	for _, r := range audienceTableRenames {
		if err := renameTableIfNeeded(tx, r[0], r[1]); err != nil {
			return fmt.Errorf("renaming %s to %s: %w", r[0], r[1], err)
		}
	}
	for _, c := range audienceColumnRenames {
		if err := renameColumnIfNeeded(tx, c.table, c.from, c.to); err != nil {
			return fmt.Errorf("renaming %s.%s to %s: %w", c.table, c.from, c.to, err)
		}
	}
	return nil
}

func tableExists(tx *gorm.DB, table string) (bool, error) {
	var exists bool
	if err := tx.Raw(`SELECT to_regclass(?) IS NOT NULL`, "public."+table).Scan(&exists).Error; err != nil {
		return false, err
	}
	return exists, nil
}

func renameTableIfNeeded(tx *gorm.DB, from, to string) error {
	var exists bool
	if err := tx.Raw(`
		SELECT to_regclass(?) IS NOT NULL AND to_regclass(?) IS NULL`,
		"public."+from, "public."+to,
	).Scan(&exists).Error; err != nil {
		return err
	}
	if !exists {
		return nil
	}
	return tx.Exec(fmt.Sprintf("ALTER TABLE %s RENAME TO %s", from, to)).Error
}

func renameColumnIfNeeded(tx *gorm.DB, table, from, to string) error {
	var exists bool
	if err := tx.Raw(`
		SELECT EXISTS (
			SELECT 1 FROM information_schema.columns
			 WHERE table_schema = 'public' AND table_name = ? AND column_name = ?
		) AND NOT EXISTS (
			SELECT 1 FROM information_schema.columns
			 WHERE table_schema = 'public' AND table_name = ? AND column_name = ?
		)`,
		table, from, table, to,
	).Scan(&exists).Error; err != nil {
		return err
	}
	if !exists {
		return nil
	}
	return tx.Exec(fmt.Sprintf("ALTER TABLE %s RENAME COLUMN %s TO %s", table, from, to)).Error
}

func renameStoredValue(tx *gorm.DB, table, column string) error {
	exists, err := tableExists(tx, table)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	return tx.Exec(fmt.Sprintf(
		"UPDATE %s SET %s = 'audience' WHERE %s = 'comment_analysis'", table, column, column,
	)).Error
}

func foldAnalysisPermissionIntoAudience(tx *gorm.DB) error {
	var exists bool
	if err := tx.Raw(
		`SELECT to_regclass(?) IS NOT NULL`, "public.workspace_member_permissions",
	).Scan(&exists).Error; err != nil {
		return err
	}
	if !exists {
		return nil
	}

	type grant struct {
		MemberID string
		Action   string
	}
	var missing []grant
	if err := tx.Raw(analysisGrantSelectSQL).Scan(&missing).Error; err != nil {
		return err
	}

	now := time.Now().UTC()
	for _, g := range missing {
		if err := tx.Exec(
			analysisGrantInsertSQL, uuid.New().String(), g.MemberID, g.Action, now,
		).Error; err != nil {
			return err
		}
	}

	return tx.Exec(`DELETE FROM workspace_member_permissions WHERE resource = 'analysis'`).Error
}
