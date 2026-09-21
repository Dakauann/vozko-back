package unofficial_whatsapp_repository

import (
	"context"
	"database/sql"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

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
		t.Fatalf("lookup must not error on an existing row: %v", err)
	}
	if got != "dept-vendas" {
		t.Errorf("department = %q, want dept-vendas", got)
	}
}

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
