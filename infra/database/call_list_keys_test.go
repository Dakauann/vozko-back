package database

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"vozko/infra/database/schema"
	"vozko/infra/repositories/repotest"
)

func TestCallListItemsKeepTheirLeadInTheSameWorkspace(t *testing.T) {
	for _, key := range compositeLeadKeys {
		if key.table == "call_list_items" && key.column == "lead_id" && key.constraint == "fk_lead_call_list_items_workspace_lead" {
			return
		}
	}
	t.Fatal("call_list_items must reference leads (workspace_id, id) like the other lead tables")
}

func TestCallListConstraintsTieItemsToTheirListAndCall(t *testing.T) {
	sqls := make([]string, 0)
	for _, c := range callListConstraints() {
		sqls = append(sqls, strings.Join(strings.Fields(c.sql), " "))
	}
	joined := strings.Join(sqls, "\n")
	for _, want := range []string{
		"FOREIGN KEY (workspace_id, list_id) REFERENCES call_lists (workspace_id, id) ON DELETE CASCADE",
		"FOREIGN KEY (last_call_id) REFERENCES calls (id) ON DELETE SET NULL",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("constraints must contain %q, got:\n%s", want, joined)
		}
	}
}

func TestCallListItemsCannotCrossWorkspacesAndGoWithTheirListAgainstPostgres(t *testing.T) {
	db := repotest.IsolatedDB(t, "call_list_keys")
	db.Config.DisableForeignKeyConstraintWhenMigrating = true
	if err := db.AutoMigrate(&schema.Lead{}, &schema.LeadPhone{}, &schema.LeadAddress{}, &schema.LeadRelation{}, &schema.Call{}, &schema.CallList{}, &schema.CallListItem{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	for i := 0; i < 2; i++ {
		if err := db.Transaction(func(tx *gorm.DB) error { return CreateCallListConstraints(tx) }); err != nil {
			t.Fatalf("constraints run %d: %v", i, err)
		}
	}
	for _, idx := range concurrentIndexes() {
		if idx.name == "ux_leads_workspace_id_id" {
			if err := createIndexConcurrently(db, idx); err != nil {
				t.Fatalf("index: %v", err)
			}
		}
	}
	if err := EnsureCompositeLeadKeys(db); err != nil {
		t.Fatalf("lead keys: %v", err)
	}

	now := time.Now().UTC()
	wsA, wsB := uuid.NewString(), uuid.NewString()
	leadA, listA := uuid.NewString(), uuid.NewString()
	execCallListSQL(t, db, "INSERT INTO leads (id, workspace_id, number, created_at, updated_at) VALUES (?, ?, '5511987654321', ?, ?)", leadA, wsA, now, now)
	execCallListSQL(t, db, "INSERT INTO call_lists (id, workspace_id, name, created_by, status, phone_source, created_at, updated_at) VALUES (?, ?, 'Lista', ?, 'active', 'identity', ?, ?)", listA, wsA, uuid.NewString(), now, now)

	insertItem := "INSERT INTO call_list_items (id, workspace_id, list_id, lead_id, phone, position, state, created_at, updated_at) VALUES (?, ?, ?, ?, '5511987654321', 1, 'pending', ?, ?)"
	if err := db.Exec(insertItem, uuid.NewString(), wsB, listA, leadA, now, now).Error; err == nil {
		t.Fatal("an item of another workspace than its list and lead must be refused")
	}
	execCallListSQL(t, db, insertItem, uuid.NewString(), wsA, listA, leadA, now, now)
	if err := db.Exec(insertItem, uuid.NewString(), wsA, listA, leadA, now, now).Error; err == nil {
		t.Fatal("the same lead twice in one list must be refused")
	}

	reserve := "UPDATE call_list_items SET state = 'reserved', reserved_by = ? WHERE id = ?"
	user := uuid.NewString()
	other := uuid.NewString()
	leadB := uuid.NewString()
	execCallListSQL(t, db, "INSERT INTO leads (id, workspace_id, number, created_at, updated_at) VALUES (?, ?, '5511912345678', ?, ?)", leadB, wsA, now, now)
	first, second := uuid.NewString(), uuid.NewString()
	db.Exec("DELETE FROM call_list_items")
	execCallListSQL(t, db, insertItem, first, wsA, listA, leadA, now, now)
	execCallListSQL(t, db, strings.Replace(insertItem, ", 1, ", ", 2, ", 1), second, wsA, listA, leadB, now, now)
	execCallListSQL(t, db, reserve, user, first)
	if err := db.Exec(reserve, user, second).Error; err == nil {
		t.Fatal("one member holds at most one reserved item per workspace")
	}
	execCallListSQL(t, db, reserve, other, second)

	execCallListSQL(t, db, "DELETE FROM call_lists WHERE id = ?", listA)
	var left int64
	db.Raw("SELECT COUNT(*) FROM call_list_items").Scan(&left)
	if left != 0 {
		t.Fatalf("deleting a list left %d items", left)
	}
}

func execCallListSQL(t *testing.T, db *gorm.DB, sql string, args ...interface{}) {
	t.Helper()
	if err := db.Exec(sql, args...).Error; err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
}
