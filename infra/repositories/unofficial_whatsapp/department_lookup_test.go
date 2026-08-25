package unofficial_whatsapp_repository

import (
	"context"
	"database/sql"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// Assignment is fail-closed on a department lookup error: it logs "cannot
// resolve department" and leaves the conversation UNASSIGNED. So a lookup that
// errors on a perfectly good row does not surface as a broken query, it
// surfaces as "this channel never assigns anyone", which is what happened here
// — and had already happened on Telegram before.
//
// The failure needs a real driver to reproduce: Pluck writes THROUGH a slice,
// so a *string destination became **string and scanning blew up with "sql: Scan
// called without calling Next" even when the row was there. A hand-rolled fake
// repository cannot catch that, which is why these go through sqlmock with the
// Postgres dialector.

func newLookupDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock, *sql.DB) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatal(err)
	}
	mock.MatchExpectationsInOrder(false)
	db, err := gorm.Open(
		postgres.New(postgres.Config{
			Conn:                 sqlDB,
			PreferSimpleProtocol: true,
			WithoutReturning:     true,
		}),
		&gorm.Config{SkipDefaultTransaction: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	return db, mock, sqlDB
}

func TestDepartmentIDForEntryReadsTheInstanceDepartment(t *testing.T) {
	db, mock, sqlDB := newLookupDB(t)
	defer sqlDB.Close()

	mock.ExpectQuery(`department_id`).
		WithArgs("conv-1", 1).
		WillReturnRows(sqlmock.NewRows([]string{"department_id"}).AddRow("dept-vendas"))

	got, err := NewConversationRepository(db).DepartmentIDForEntry(context.Background(), "conv-1")
	if err != nil {
		// The regression: this errored with "sql: Scan called without calling
		// Next" on a row that exists, and every inbound message went unassigned.
		t.Fatalf("lookup must not error on an existing row: %v", err)
	}
	if got != "dept-vendas" {
		t.Errorf("department = %q, want dept-vendas", got)
	}
}

// A NULL department is a real configuration — an instance not scoped to any
// department — and must read as "no scope", not as a failure. Assignment then
// falls back to the whole workspace instead of assigning nobody.
func TestDepartmentIDForEntryTreatsNullAsNoScope(t *testing.T) {
	db, mock, sqlDB := newLookupDB(t)
	defer sqlDB.Close()

	mock.ExpectQuery(`department_id`).
		WithArgs("conv-1", 1).
		WillReturnRows(sqlmock.NewRows([]string{"department_id"}).AddRow(nil))

	got, err := NewConversationRepository(db).DepartmentIDForEntry(context.Background(), "conv-1")
	if err != nil {
		t.Fatalf("a NULL department is not an error: %v", err)
	}
	if got != "" {
		t.Errorf("department = %q, want empty for NULL", got)
	}
}

// An entry that resolves to no row must also be "no scope" rather than an
// error, for the same fail-closed reason.
func TestDepartmentIDForEntryTreatsMissingRowAsNoScope(t *testing.T) {
	db, mock, sqlDB := newLookupDB(t)
	defer sqlDB.Close()

	mock.ExpectQuery(`department_id`).
		WithArgs("ghost", 1).
		WillReturnRows(sqlmock.NewRows([]string{"department_id"}))

	got, err := NewConversationRepository(db).DepartmentIDForEntry(context.Background(), "ghost")
	if err != nil {
		t.Fatalf("a missing row is not an error: %v", err)
	}
	if got != "" {
		t.Errorf("department = %q, want empty", got)
	}
}
