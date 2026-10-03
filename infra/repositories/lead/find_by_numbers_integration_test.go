package lead

import (
	"testing"

	"github.com/google/uuid"

	"vozko/infra/repositories/repotest"
)

func TestContactsAreFoundByAnyFormatOfTheirNumbersInsideTheWorkspace(t *testing.T) {
	db := repotest.IsolatedDB(t, "leads")
	if err := db.Exec(`CREATE TABLE leads (
		id uuid PRIMARY KEY, workspace_id uuid NOT NULL, number varchar(20) NOT NULL, name varchar(255),
		profile_picture_url varchar(1024), age int, blocked bool NOT NULL DEFAULT false, blocked_at timestamptz,
		blocked_by uuid, created_at timestamptz, updated_at timestamptz, deleted_at timestamptz)`).Error; err != nil {
		t.Fatal(err)
	}
	ws, other := uuid.NewString(), uuid.NewString()
	insert := func(workspaceID, number, name string) {
		if err := db.Exec(`INSERT INTO leads (id, workspace_id, number, name, created_at, updated_at) VALUES (?, ?, ?, ?, now(), now())`,
			uuid.NewString(), workspaceID, number, name).Error; err != nil {
			t.Fatal(err)
		}
	}
	insert(ws, "558494409684", "Maria")
	insert(ws, "5511999990000", "João")
	insert(other, "5511888880000", "Stranger")

	repo := &repository{db: db}
	found, err := repo.FindByNumbers(ws, []string{"5584994409684", "5511888880000", "100"})
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 || found[0].Name != "Maria" {
		t.Fatalf("found = %+v, want only Maria, matched through the other mobile format", found)
	}
	if none, err := repo.FindByNumbers(ws, []string{"100"}); err != nil || len(none) != 0 {
		t.Fatalf("unmatchable numbers = %+v, %v", none, err)
	}
}
