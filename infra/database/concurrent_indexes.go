package database

import (
	"log"

	"gorm.io/gorm"
)

type concurrentIndex struct {
	name string
	sql  string
}

func concurrentIndexes() []concurrentIndex {
	return []concurrentIndex{
		{
			name: "idx_conv_event_ws_type_created",
			sql: `CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_conv_event_ws_type_created
				ON conversation_events (workspace_id, event_type, created_at)`,
		},
		{
			name: "idx_ca_waiting",
			sql: `CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_ca_waiting
				ON audience_analyses (workspace_id)
				WHERE status IN ('pending', 'in_flight') AND deleted_at IS NULL`,
		},
	}
}

func createConcurrentIndexes(db *gorm.DB) {
	for _, idx := range concurrentIndexes() {
		if err := createIndexConcurrently(db, idx); err != nil {
			log.Printf("[indexes] Warning: failed to create %s: %v", idx.name, err)
		}
	}
}

func createIndexConcurrently(db *gorm.DB, idx concurrentIndex) error {
	var invalid bool
	err := db.Raw(`
		SELECT EXISTS (
			SELECT 1
			FROM pg_class c
			JOIN pg_index i ON i.indexrelid = c.oid
			WHERE c.relname = ? AND NOT i.indisvalid
		)
	`, idx.name).Scan(&invalid).Error
	if err != nil {
		return err
	}
	if invalid {
		if err := db.Exec("DROP INDEX CONCURRENTLY IF EXISTS " + idx.name).Error; err != nil {
			return err
		}
	}
	return db.Exec(idx.sql).Error
}
