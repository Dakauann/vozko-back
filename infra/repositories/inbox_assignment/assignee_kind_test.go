package inbox_assignment_repository

import (
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	ia "vozko/domain/inbox_assignment"
)

const agentUUID = "7a1c3f0e-0000-4000-8000-00000000a1a1"

func assignmentColumns() []string {
	return []string{"id", "workspace_id", "business_phone_id", "entry_id", "entry_type", "assigned_user_id", "assignee_kind", "created_at", "updated_at"}
}

func TestAssign_StoresTheAIAsItsBareIDAndKind(t *testing.T) {
	db, mock, sqlDB := newRRDB(t)
	defer sqlDB.Close()

	mock.ExpectExec(`INSERT INTO "inbox_assignments" .*"assigned_user_id","assignee_kind".*ON CONFLICT .*"assignee_kind"="excluded"."assignee_kind"`).
		WithArgs(sqlmock.AnyArg(), "ws-1", "entry-1", "whatsapp", agentUUID, "ai", sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := New(db).Assign(&ia.InboxAssignment{
		WorkspaceID:    "ws-1",
		EntryID:        "entry-1",
		EntryType:      "whatsapp",
		AssignedUserID: "ai:" + agentUUID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestAssign_AHumanIsStoredAsHuman(t *testing.T) {
	db, mock, sqlDB := newRRDB(t)
	defer sqlDB.Close()

	mock.ExpectExec(`INSERT INTO "inbox_assignments" .*ON CONFLICT .*"assignee_kind"="excluded"."assignee_kind"`).
		WithArgs(sqlmock.AnyArg(), "ws-1", "entry-1", "whatsapp", "user-1", "human", sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := New(db).Assign(&ia.InboxAssignment{
		WorkspaceID: "ws-1", EntryID: "entry-1", EntryType: "whatsapp", AssignedUserID: "user-1",
	}); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestFindByEntry_ReadsTheAIBackWithItsPrefix(t *testing.T) {
	db, mock, sqlDB := newRRDB(t)
	defer sqlDB.Close()

	now := time.Now()
	mock.ExpectQuery(`SELECT \* FROM "inbox_assignments"`).
		WillReturnRows(sqlmock.NewRows(assignmentColumns()).
			AddRow("a-1", "ws-1", nil, "entry-1", "whatsapp", agentUUID, "ai", now, now))

	got, err := New(db).FindByEntry("ws-1", "entry-1", "whatsapp")
	if err != nil {
		t.Fatal(err)
	}
	if got.AssignedUserID != "ai:"+agentUUID {
		t.Fatalf("AssignedUserID = %q, want ai:%s", got.AssignedUserID, agentUUID)
	}
	if !got.HeldByAutomation() {
		t.Fatal("an ai row must read back as held by the AI")
	}
}

func TestFindByEntry_RowsBeforeTheColumnAreHuman(t *testing.T) {
	db, mock, sqlDB := newRRDB(t)
	defer sqlDB.Close()

	now := time.Now()
	mock.ExpectQuery(`SELECT \* FROM "inbox_assignments"`).
		WillReturnRows(sqlmock.NewRows(assignmentColumns()).
			AddRow("a-1", "ws-1", nil, "entry-1", "whatsapp", "user-1", "human", now, now))

	got, err := New(db).FindByEntry("ws-1", "entry-1", "whatsapp")
	if err != nil {
		t.Fatal(err)
	}
	if got.AssignedUserID != "user-1" || got.HeldByAutomation() {
		t.Fatalf("got %+v, want a human assignment to user-1", got)
	}
}

func TestIsAssignedToUser_AsksForTheKindAndTheBareID(t *testing.T) {
	cases := []struct {
		name   string
		id     string
		wantID string
		kind   string
	}{
		{name: "person", id: "user-1", wantID: "user-1", kind: "human"},
		{name: "ai", id: "ai:" + agentUUID, wantID: agentUUID, kind: "ai"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, sqlDB := newRRDB(t)
			defer sqlDB.Close()

			mock.ExpectQuery(`SELECT count\(\*\) FROM "inbox_assignments" WHERE .*assigned_user_id = \$\d+ AND assignee_kind = \$\d+`).
				WithArgs("ws-1", "entry-1", "whatsapp", tc.wantID, tc.kind).
				WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

			ok, err := New(db).IsAssignedToUser("ws-1", "entry-1", "whatsapp", tc.id)
			if err != nil || !ok {
				t.Fatalf("IsAssignedToUser = %v, %v", ok, err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestListByUser_AsksForTheKind(t *testing.T) {
	db, mock, sqlDB := newRRDB(t)
	defer sqlDB.Close()

	mock.ExpectQuery(`SELECT "entry_id" FROM "inbox_assignments" WHERE .*assigned_user_id = \$\d+ AND assignee_kind = \$\d+`).
		WithArgs("ws-1", "user-1", "human").
		WillReturnRows(sqlmock.NewRows([]string{"entry_id"}).AddRow("entry-1"))

	ids, err := New(db).ListByUser("ws-1", "user-1", "")
	if err != nil || len(ids) != 1 {
		t.Fatalf("ListByUser = %v, %v", ids, err)
	}
}
