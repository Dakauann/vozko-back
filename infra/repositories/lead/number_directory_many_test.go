package lead

import (
	"context"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestLoadManyForDialReadsThePageOfLeadsAndTheirPhonesInTwoArrayBoundQueries(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	ids := make([]string, 0, 5000)
	for i := 0; i < 5000; i++ {
		ids = append(ids, leadUUID)
	}
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "leads" WHERE workspace_id = $1 AND id = ANY($2::uuid[]) AND "leads"."deleted_at" IS NULL`)).
		WithArgs(wsUUID, sqlmock.AnyArg()).
		WillReturnRows(leadRow("Maria", "manual", 4))
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "lead_phones" WHERE workspace_id = $1 AND lead_id = ANY($2::uuid[]) ORDER BY lead_id, position`)).
		WithArgs(wsUUID, sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id", "workspace_id", "lead_id", "number", "label", "position"}).
			AddRow(phoneUUID, wsUUID, leadUUID, "551133334444", "landline", 0))

	got, err := NewNumberDirectory(db).LoadManyForDial(context.Background(), wsUUID, ids)
	if err != nil {
		t.Fatalf("LoadManyForDial: %v", err)
	}
	if len(got) != 1 || len(got[0].Phones) != 1 || got[0].Addresses != nil {
		t.Fatalf("leads = %+v", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
