package database

import (
	"strings"
	"testing"
)

func TestPerformanceIndexSQL_GivesTheDeclaredStatement(t *testing.T) {
	sql, ok := PerformanceIndexSQL("idx_leads_workspace_created")
	if !ok || !strings.Contains(sql, "WHERE deleted_at IS NULL") {
		t.Fatalf("PerformanceIndexSQL(idx_leads_workspace_created) = %q, %v", sql, ok)
	}
	if _, ok := PerformanceIndexSQL("idx_never_declared"); ok {
		t.Fatal("an undeclared index must not be found")
	}
}

func TestPerformanceIndexSQL_AlsoFindsTheIndexesCreatedWithTheSchema(t *testing.T) {
	sql, ok := PerformanceIndexSQL("idx_lead_memories_lead_id")
	if !ok || !strings.Contains(sql, "ON lead_memories (lead_id)") {
		t.Fatalf("PerformanceIndexSQL(idx_lead_memories_lead_id) = %q, %v", sql, ok)
	}
}
