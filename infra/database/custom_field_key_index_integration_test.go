package database

import (
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"vozko/infra/database/schema"
	"vozko/infra/repositories/repotest"
)

func liveKeyIndex(t *testing.T) concurrentIndex {
	t.Helper()
	for _, idx := range concurrentIndexes() {
		if idx.name == schema.CustomFieldLiveKeyIndex {
			return idx
		}
	}
	t.Fatalf("%s is not built", schema.CustomFieldLiveKeyIndex)
	return concurrentIndex{}
}

func indexExists(t *testing.T, db *gorm.DB, name string) bool {
	t.Helper()
	var exists bool
	if err := db.Raw(`SELECT to_regclass(?::text) IS NOT NULL`, name).Scan(&exists).Error; err != nil {
		t.Fatalf("lookup %s: %v", name, err)
	}
	return exists
}

func insertDefinition(db *gorm.DB, workspaceID, key string) (string, error) {
	id := uuid.New().String()
	err := db.Exec(`INSERT INTO custom_field_definitions (id, workspace_id, object_type, key, label, type, created_at, updated_at)
		VALUES (?, ?, 'lead', ?, 'Rótulo', 'text', NOW(), NOW())`, id, workspaceID, key).Error
	return id, err
}

func TestDeletedCustomFieldKeysCanBeRecreatedAfterTheIndexSwap(t *testing.T) {
	db := repotest.IsolatedDB(t, "cfkey", &schema.CustomFieldDefinition{})
	if err := db.Exec(`CREATE UNIQUE INDEX idx_custom_field_ws_object_key ON custom_field_definitions (workspace_id, object_type, key)`).Error; err != nil {
		t.Fatalf("legacy index fixture: %v", err)
	}

	if err := createIndexConcurrently(db, liveKeyIndex(t)); err != nil {
		t.Fatalf("createIndexConcurrently() error = %v", err)
	}
	if indexExists(t, db, "idx_custom_field_ws_object_key") {
		t.Fatal("the legacy non partial index must be dropped once the new one is valid")
	}
	if !indexExists(t, db, schema.CustomFieldLiveKeyIndex) {
		t.Fatal("the partial index was not built")
	}

	ws := uuid.New().String()
	id, err := insertDefinition(db, ws, "classificacao")
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	if _, err := insertDefinition(db, ws, "classificacao"); err == nil || !IsUniqueViolation(err) {
		t.Fatalf("a second live definition with the same key must be refused, got %v", err)
	}
	if err := db.Exec(`UPDATE custom_field_definitions SET deleted_at = NOW() WHERE id = ?`, id).Error; err != nil {
		t.Fatalf("soft delete: %v", err)
	}
	if _, err := insertDefinition(db, ws, "classificacao"); err != nil {
		t.Fatalf("a deleted key must be reusable: %v", err)
	}

	if err := createIndexConcurrently(db, liveKeyIndex(t)); err != nil {
		t.Fatalf("a second boot must be a no op, got %v", err)
	}
}
