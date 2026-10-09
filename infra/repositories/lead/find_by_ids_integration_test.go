package lead

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"vozko/infra/repositories/repotest"
)

func TestFindByIDsReadsOnlyTheWorkspaceAcrossSeventyThousandIDs(t *testing.T) {
	db := repotest.IsolatedDB(t, "leads")
	if err := db.Exec(`CREATE TABLE leads (
		id uuid PRIMARY KEY, workspace_id uuid NOT NULL, number varchar(20) NOT NULL, name varchar(255),
		profile_picture_url varchar(1024), age int, blocked bool NOT NULL DEFAULT false, blocked_at timestamptz,
		blocked_by uuid, created_at timestamptz, updated_at timestamptz, deleted_at timestamptz)`).Error; err != nil {
		t.Fatal(err)
	}
	ws, other := uuid.NewString(), uuid.NewString()
	mine, foreign, gone := uuid.NewString(), uuid.NewString(), uuid.NewString()
	removed := time.Now()
	for _, row := range []struct {
		id, workspace string
		deletedAt     *time.Time
	}{{mine, ws, nil}, {foreign, other, nil}, {gone, ws, &removed}} {
		if err := db.Exec(`INSERT INTO leads (id, workspace_id, number, created_at, updated_at, deleted_at) VALUES (?, ?, '5584994409684', now(), now(), ?)`,
			row.id, row.workspace, row.deletedAt).Error; err != nil {
			t.Fatal(err)
		}
	}

	ids := append(manyLeadIDs(70_000), mine, foreign, gone, "not-a-uuid")
	found, err := (&repository{db: db}).FindByIDs(ws, ids)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 || found[0].ID != mine {
		t.Fatalf("found = %+v, want only the live lead of the workspace", found)
	}
}
