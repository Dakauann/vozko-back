package database

import (
	"strings"
	"testing"

	"vozko/infra/database/schema"
)

func TestCustomFieldRolesAreUniqueAmongLiveDefinitionsThatHoldOne(t *testing.T) {
	sql, ok := ConcurrentIndexSQL(schema.CustomFieldLiveRoleIndex)
	if !ok {
		t.Fatalf("%s is not built", schema.CustomFieldLiveRoleIndex)
	}
	want := "CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS " + schema.CustomFieldLiveRoleIndex +
		" ON custom_field_definitions (workspace_id, object_type, role) WHERE role IS NOT NULL AND role <> '' AND deleted_at IS NULL"
	if got := strings.Join(strings.Fields(sql), " "); got != want {
		t.Fatalf("sql = %q, want %q", got, want)
	}
}

func TestLeadCustomFieldsAreIndexedForTopLevelContainment(t *testing.T) {
	sql, ok := ConcurrentIndexSQL(LeadCustomFieldsIndex)
	if !ok {
		t.Fatalf("%s is not built", LeadCustomFieldsIndex)
	}
	want := "CREATE INDEX CONCURRENTLY IF NOT EXISTS " + LeadCustomFieldsIndex +
		" ON leads USING gin (custom_fields jsonb_path_ops) WHERE custom_fields IS NOT NULL"
	if got := strings.Join(strings.Fields(sql), " "); got != want {
		t.Fatalf("sql = %q, want %q", got, want)
	}
}
