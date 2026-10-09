package lead

import (
	"context"
	"errors"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"vozko/domain/lead"
)

func TestLoadForDialReadsTheLeadAndItsPhonesOnly(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "leads" WHERE workspace_id = $1 AND id = $2 AND "leads"."deleted_at" IS NULL ORDER BY "leads"."id" LIMIT $3`)).
		WithArgs(wsUUID, leadUUID, 1).
		WillReturnRows(leadRow("Maria", "manual", 4))
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "lead_phones" WHERE workspace_id = $1 AND lead_id = ANY($2::uuid[]) ORDER BY lead_id, position`)).
		WithArgs(wsUUID, sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id", "workspace_id", "lead_id", "number", "label", "position"}).
			AddRow(phoneUUID, wsUUID, leadUUID, "551133334444", "landline", 0))

	got, err := NewNumberDirectory(db).LoadForDial(context.Background(), wsUUID, leadUUID)
	if err != nil {
		t.Fatalf("LoadForDial: %v", err)
	}
	if got.ID != leadUUID || len(got.Phones) != 1 || !got.HoldsNumber("551133334444") {
		t.Fatalf("lead = %+v", got)
	}
	if got.Addresses != nil {
		t.Fatal("a dial check never reads addresses")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestLoadForDialAnswersNotFoundWithoutQueryingAMalformedID(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	if _, err := NewNumberDirectory(db).LoadForDial(context.Background(), wsUUID, "lead-1"); !errors.Is(err, lead.ErrLeadNotFound) {
		t.Fatalf("LoadForDial = %v, want not found", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestTheDirectoryFindsLinkedLeadsByOneArrayBoundID(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "leads" WHERE workspace_id = $1 AND id = ANY($2::uuid[]) AND "leads"."deleted_at" IS NULL`)).
		WithArgs(wsUUID, sqlmock.AnyArg()).
		WillReturnRows(leadRow("Maria", "manual", 4))

	got, err := NewNumberDirectory(db).FindByIDs(wsUUID, []string{leadUUID, leadUUID, "not-a-uuid"})
	if err != nil || len(got) != 1 || got[0].ID != leadUUID {
		t.Fatalf("FindByIDs = %+v, %v", got, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestFindIdentitiesReadsOnlyTheWhatsAppHoldersOfTheNumbersInOneArray(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "leads" WHERE (workspace_id = $1 AND number = ANY($2::text[])) AND "leads"."deleted_at" IS NULL LIMIT $3`)).
		WithArgs(wsUUID, sqlmock.AnyArg(), directoryLookupLimit).
		WillReturnRows(leadRow("Maria", "manual", 4))

	got, err := NewNumberDirectory(db).FindIdentities(context.Background(), wsUUID, []string{"5511987654321", "551133334444"})
	if err != nil || len(got) != 1 || got[0].ID != leadUUID || got[0].Phones != nil {
		t.Fatalf("FindIdentities = %+v, %v, want the holder without its phones", got, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestFindIdentitiesQueriesNothingWithoutANumberOrAWorkspace(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	directory := NewNumberDirectory(db)
	if got, err := directory.FindIdentities(context.Background(), wsUUID, []string{" "}); err != nil || len(got) != 0 {
		t.Fatalf("FindIdentities = %+v, %v, want nobody", got, err)
	}
	if _, err := directory.FindIdentities(context.Background(), " ", []string{"5511987654321"}); !errors.Is(err, lead.ErrLeadWorkspaceRequired) {
		t.Fatalf("FindIdentities = %v, want the workspace required", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
