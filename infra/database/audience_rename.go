package database

import (
	"fmt"

	"gorm.io/gorm"
)

// The comment-analysis engine became the audience engine, and the schema
// follows the code.
//
// Everything here is a RENAME, never a create-and-copy: the tables hold
// customers' analysed history and their spend receipts, and a migration that
// created new tables beside the old ones would leave every row behind while the
// application read empty tables, silently and with no error to notice.
//
// Every statement is guarded on the CURRENT state rather than on a version
// number, so running it twice is a no-op and running it against a database that
// was created fresh under the new names is also a no-op.

// audienceTableRenames is the old name to new name map, in no particular order:
// renaming one table cannot affect another.
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

// audienceColumnRenames are the columns whose names described a comment
// specifically, on a table that now holds conversations too.
var audienceColumnRenames = []struct{ table, from, to string }{
	{"audience_analyses", "source_comment_id", "subject_id"},
	{"audience_analyses", "parent_comment_id", "parent_subject_id"},
	{"audience_analyses", "commented_at", "occurred_at"},
}

func renameCommentAnalysisToAudience(tx *gorm.DB) error {
	for _, r := range audienceTableRenames {
		if err := renameTableIfNeeded(tx, r[0], r[1]); err != nil {
			return fmt.Errorf("renaming %s to %s: %w", r[0], r[1], err)
		}
	}
	// Columns AFTER the tables, so they are addressed by their new table name.
	for _, c := range audienceColumnRenames {
		if err := renameColumnIfNeeded(tx, c.table, c.from, c.to); err != nil {
			return fmt.Errorf("renaming %s.%s to %s: %w", c.table, c.from, c.to, err)
		}
	}
	return nil
}

// renameTableIfNeeded renames only when the old table exists and the new one
// does not.
//
// Both halves of that condition matter. Without the first, a fresh database
// fails on a table that was never there. Without the second, a database where
// AutoMigrate has already created the new table would try to rename onto an
// occupied name and abort the whole migration transaction, taking the boot
// with it.
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

// renameColumnIfNeeded is the same guard one level down. The table may not
// exist at all on a fresh database, which is why the column lookup is the
// condition rather than the table's presence.
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
