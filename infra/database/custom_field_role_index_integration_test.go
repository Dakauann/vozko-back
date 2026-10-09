package database

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"vozko/infra/database/schema"
	"vozko/infra/repositories/repotest"
)

func buildConcurrentIndex(t *testing.T, db *gorm.DB, name string) {
	t.Helper()
	for _, idx := range concurrentIndexes() {
		if idx.name == name {
			if err := createIndexConcurrently(db, idx); err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			return
		}
	}
	t.Fatalf("%s is not built", name)
}

func insertRoleDefinition(db *gorm.DB, workspaceID, objectType, key string, role any) (string, error) {
	id := uuid.New().String()
	err := db.Exec(`INSERT INTO custom_field_definitions (id, workspace_id, object_type, key, label, type, role, created_at, updated_at)
		VALUES (?, ?, ?, ?, 'Rótulo', 'select', ?, NOW(), NOW())`, id, workspaceID, objectType, key, role).Error
	return id, err
}

func TestOneLiveDefinitionPerRoleAgainstPostgres(t *testing.T) {
	db := repotest.IsolatedDB(t, "cfrole", &schema.CustomFieldDefinition{})
	for i := 0; i < 2; i++ {
		buildConcurrentIndex(t, db, schema.CustomFieldLiveRoleIndex)
	}
	ws := uuid.New().String()

	first, err := insertRoleDefinition(db, ws, "lead", "classificacao", "classification")
	if err != nil {
		t.Fatalf("first classification: %v", err)
	}
	_, err = insertRoleDefinition(db, ws, "lead", "interesse", "classification")
	if !IsUniqueViolationOf(err, schema.CustomFieldLiveRoleIndex) {
		t.Fatalf("a second live classification must be refused by %s, got %v", schema.CustomFieldLiveRoleIndex, err)
	}
	if _, err := insertRoleDefinition(db, ws, "opportunity", "classificacao", "classification"); err != nil {
		t.Fatalf("another object may hold the same role: %v", err)
	}
	if _, err := insertRoleDefinition(db, uuid.New().String(), "lead", "classificacao", "classification"); err != nil {
		t.Fatalf("another workspace may hold the same role: %v", err)
	}
	for i, role := range []any{"", "", nil, nil} {
		if _, err := insertRoleDefinition(db, ws, "lead", "livre_"+string(rune('a'+i)), role); err != nil {
			t.Fatalf("definitions without a role must coexist (%v): %v", role, err)
		}
	}
	if err := db.Exec(`UPDATE custom_field_definitions SET deleted_at = NOW() WHERE id = ?`, first).Error; err != nil {
		t.Fatalf("soft delete: %v", err)
	}
	if _, err := insertRoleDefinition(db, ws, "lead", "interesse", "classification"); err != nil {
		t.Fatalf("the role of a deleted definition must be free again: %v", err)
	}
}

func TestLeadCustomFieldContainmentReadsTheIndexAgainstPostgres(t *testing.T) {
	db := repotest.IsolatedDB(t, "lead_cf_gin")
	db.Config.DisableForeignKeyConstraintWhenMigrating = true
	if err := db.AutoMigrate(&schema.Lead{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	for i := 0; i < 2; i++ {
		buildConcurrentIndex(t, db, LeadCustomFieldsIndex)
	}
	ws := uuid.New().String()
	if err := db.Exec(`INSERT INTO leads (id, workspace_id, number, custom_fields, created_at, updated_at)
		SELECT gen_random_uuid(), ?, '5511' || lpad(g::text, 9, '0'),
		       CASE WHEN g % 2 = 0 THEN jsonb_build_object('interesse', (ARRAY['alto', 'baixo'])[1 + g % 2]) END, NOW(), NOW()
		FROM generate_series(1, 2000) g`, ws).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	var plan []string
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SET LOCAL enable_seqscan = off").Error; err != nil {
			return err
		}
		return tx.Raw(`EXPLAIN SELECT id FROM leads WHERE custom_fields @> jsonb_build_object(?::text, ?::text)`, "interesse", "alto").Scan(&plan).Error
	})
	if err != nil {
		t.Fatalf("explain: %v", err)
	}
	if joined := strings.Join(plan, "\n"); !strings.Contains(joined, LeadCustomFieldsIndex) {
		t.Fatalf("the containment filter the compiler emits must be able to read %s:\n%s", LeadCustomFieldsIndex, joined)
	}
}
