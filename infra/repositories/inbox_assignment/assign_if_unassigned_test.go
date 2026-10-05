package inbox_assignment_repository

import (
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	ia "vozko/domain/inbox_assignment"
)

func freeClaim() *ia.InboxAssignment {
	return &ia.InboxAssignment{
		WorkspaceID: "ws-1", BusinessPhoneID: "phone-1",
		EntryID: "11111111-1111-1111-1111-111111111111", EntryType: "whatsapp", AssignedUserID: "22222222-2222-2222-2222-222222222222",
	}
}

func TestAssignIfUnassignedNeverOverwritesAnOwner(t *testing.T) {
	db, mock, sqlDB := newRRDB(t)
	defer sqlDB.Close()

	mock.ExpectExec(`INSERT INTO "inbox_assignments" .* ON CONFLICT \("entry_id","entry_type"\) DO NOTHING`).
		WillReturnResult(sqlmock.NewResult(0, 1))

	claimed, err := New(db).AssignIfUnassigned(freeClaim())
	if err != nil {
		t.Fatal(err)
	}
	if !claimed {
		t.Fatal("an inserted row is a claim")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestAssignIfUnassignedReportsALostRace(t *testing.T) {
	db, mock, sqlDB := newRRDB(t)
	defer sqlDB.Close()

	mock.ExpectExec(`INSERT INTO "inbox_assignments"`).
		WillReturnResult(sqlmock.NewResult(0, 0))

	claimed, err := New(db).AssignIfUnassigned(freeClaim())
	if err != nil {
		t.Fatal(err)
	}
	if claimed {
		t.Fatal("a conversation someone else took first must not be reported as claimed")
	}
}
