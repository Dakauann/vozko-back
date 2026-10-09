package inbox_assignment_repository

import (
	"fmt"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestFindByEntriesBindsTheEntryIDsAsOneArray(t *testing.T) {
	db, mock, sqlDB := newRRDB(t)
	defer sqlDB.Close()

	ids := make([]string, 1200)
	for i := range ids {
		ids[i] = fmt.Sprintf("00000000-0000-4000-8000-%012d", i)
	}
	mock.ExpectQuery(`SELECT \* FROM "inbox_assignments" WHERE workspace_id = \$1 AND entry_id = ANY\(\$2::uuid\[\]\)$`).
		WithArgs("ws-1", sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows(assignmentColumns()).AddRow("a-1", "ws-1", nil, ids[3], "whatsapp", agentUUID, "ai", nil, nil))

	got, err := New(db).FindByEntries("ws-1", ids)
	if err != nil || len(got) != 1 || got[0].AssignedUserID != "ai:"+agentUUID {
		t.Fatalf("FindByEntries = %+v, %v", got, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestFindByEntriesWithoutAValidIDReadsNothing(t *testing.T) {
	db, _, sqlDB := newRRDB(t)
	defer sqlDB.Close()
	got, err := New(db).FindByEntries("ws-1", []string{"", "not-a-uuid"})
	if err != nil || len(got) != 0 {
		t.Fatalf("FindByEntries = %+v, %v", got, err)
	}
}
