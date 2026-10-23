package carriermessaging_repository

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"vozko/domain/carriermessaging"
)

var contactColumns = []string{"id", "workspace_id", "phone", "display_name", "lead_id", "flooding_at", "created_at", "updated_at", "deleted_at"}

func contactRows() *sqlmock.Rows {
	return sqlmock.NewRows(contactColumns).AddRow("ct1", "ws1", "5511999990000", "Ana", "l1", nil, testNow, testNow, nil)
}

func TestContactFindOrCreateReturnsTheExistingContact(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "carrier_contacts" WHERE \(workspace_id = \$1 AND phone = \$2\) AND "carrier_contacts"."deleted_at" IS NULL`).
		WithArgs("ws1", "5511999990000", 1).WillReturnRows(contactRows())
	got, err := NewContactRepository(db).FindOrCreate(context.Background(), "ws1", "5511999990000", "Ana")
	if err != nil || got.ID != "ct1" || got.LeadID == nil || *got.LeadID != "l1" {
		t.Fatalf("got %+v %v", got, err)
	}
	expectationsMet(t, mock)
}

func TestContactFindOrCreateInsertsRaceSafelyThenReads(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "carrier_contacts"`).WillReturnRows(sqlmock.NewRows(contactColumns))
	mock.ExpectExec(`INSERT INTO "carrier_contacts" .* ON CONFLICT \("workspace_id","phone"\) WHERE deleted_at IS NULL DO NOTHING`).
		WithArgs(sqlmock.AnyArg(), "ws1", "5511999990000", "Ana", nil, nil, sqlmock.AnyArg(), sqlmock.AnyArg(), nil).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`SELECT \* FROM "carrier_contacts"`).WillReturnRows(contactRows())
	got, err := NewContactRepository(db).FindOrCreate(context.Background(), "ws1", "5511999990000", " Ana ")
	if err != nil || got.ID != "ct1" {
		t.Fatalf("got %+v %v", got, err)
	}
	expectationsMet(t, mock)
}

func TestContactFindByID(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "carrier_contacts" WHERE id = \$1`).WithArgs("ct1", 1).WillReturnRows(contactRows())
	if got, err := NewContactRepository(db).FindByID(context.Background(), "ct1"); err != nil || got.Phone != "5511999990000" {
		t.Fatalf("got %+v %v", got, err)
	}
	mock.ExpectQuery(`SELECT \* FROM "carrier_contacts"`).WillReturnRows(sqlmock.NewRows(contactColumns))
	if _, err := NewContactRepository(db).FindByID(context.Background(), "nope"); !errors.Is(err, carriermessaging.ErrContactNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestContactFindByIDs(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "carrier_contacts" WHERE id IN \(\$1,\$2\)`).WithArgs("ct1", "ct2").WillReturnRows(contactRows())
	got, err := NewContactRepository(db).FindByIDs(context.Background(), []string{"ct1", "ct2", "ct1"})
	if err != nil || len(got) != 1 {
		t.Fatalf("got %v %v", got, err)
	}
	expectationsMet(t, mock)
}

func TestContactSetLead(t *testing.T) {
	cases := []struct {
		name     string
		leadID   string
		arg      any
		affected int64
		want     error
	}{
		{"links", "l1", "l1", 1, nil},
		{"unlinks", "", nil, 1, nil},
		{"missing", "l1", "l1", 0, carriermessaging.ErrContactNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, mock := newMockDB(t)
			mock.ExpectExec(`UPDATE "carrier_contacts" SET "lead_id"=\$1,"updated_at"=\$2 WHERE id = \$3`).
				WithArgs(tc.arg, sqlmock.AnyArg(), "ct1").WillReturnResult(sqlmock.NewResult(0, tc.affected))
			if err := NewContactRepository(db).SetLead(context.Background(), "ct1", tc.leadID); !errors.Is(err, tc.want) {
				t.Fatalf("got %v", err)
			}
			expectationsMet(t, mock)
		})
	}
}

func TestContactMarkFloodingOnlyOnce(t *testing.T) {
	cases := []struct {
		name     string
		affected int64
		exists   *int
		want     bool
		wantErr  error
	}{
		{"marks", 1, nil, true, nil},
		{"already marked", 0, ptr(1), false, nil},
		{"missing", 0, ptr(0), false, carriermessaging.ErrContactNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, mock := newMockDB(t)
			mock.ExpectExec(`UPDATE "carrier_contacts" SET "flooding_at"=\$1,"updated_at"=\$2 WHERE \(id = \$3 AND flooding_at IS NULL\) AND "carrier_contacts"."deleted_at" IS NULL`).
				WithArgs(testNow, sqlmock.AnyArg(), "ct1").WillReturnResult(sqlmock.NewResult(0, tc.affected))
			if tc.exists != nil {
				mock.ExpectQuery(`SELECT count\(\*\) FROM "carrier_contacts" WHERE id = \$1`).
					WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(*tc.exists))
			}
			got, err := NewContactRepository(db).MarkFlooding(context.Background(), "ct1", testNow)
			if got != tc.want || !errors.Is(err, tc.wantErr) {
				t.Fatalf("got %v %v", got, err)
			}
			expectationsMet(t, mock)
		})
	}
}
