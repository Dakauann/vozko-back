package whatsapp_campaign_entry

import (
	"fmt"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestEntryPlacementsReadsAnyNumberOfEntriesInOneArrayBoundQuery(t *testing.T) {
	repo, mock, sqlDB := newEntryDB(t)
	defer sqlDB.Close()

	ids := make([]string, 500)
	for i := range ids {
		ids[i] = fmt.Sprintf("00000000-0000-4000-8000-%012d", i)
	}
	mock.ExpectQuery(`SELECT whatsapp_campaign_entries\.id::text AS entry_id, whatsapp_campaigns\.workspace_id::text AS workspace_id, ` +
		`COALESCE\(whatsapp_campaigns\.department_id::text, ''\) AS department_id FROM whatsapp_campaign_entries ` +
		`JOIN whatsapp_campaigns ON whatsapp_campaigns\.id = whatsapp_campaign_entries\.campaign_id AND whatsapp_campaigns\.deleted_at IS NULL ` +
		`WHERE whatsapp_campaign_entries\.id = ANY\(\$1::uuid\[\]\) AND whatsapp_campaign_entries\.deleted_at IS NULL$`).
		WithArgs(sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"entry_id", "workspace_id", "department_id"}).
			AddRow(ids[0], "ws-a", "dept-1").
			AddRow(ids[1], "ws-a", ""))

	placements, err := (&PlacementDirectory{db: repo.db}).EntryPlacements(ids)
	if err != nil {
		t.Fatalf("EntryPlacements: %v", err)
	}
	if len(placements) != 2 || placements[ids[0]].DepartmentID != "dept-1" || placements[ids[1]].WorkspaceID != "ws-a" {
		t.Fatalf("placements = %+v", placements)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestEntryPlacementsOfNothingReadsNothing(t *testing.T) {
	repo, _, sqlDB := newEntryDB(t)
	defer sqlDB.Close()
	placements, err := (&PlacementDirectory{db: repo.db}).EntryPlacements([]string{"not-a-uuid"})
	if err != nil || len(placements) != 0 {
		t.Fatalf("placements = %+v, %v", placements, err)
	}
}
