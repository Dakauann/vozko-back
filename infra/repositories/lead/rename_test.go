package lead

import (
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"vozko/domain/lead"
)

func newRenameDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock, *sql.DB) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatal(err)
	}
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

func TestRename_GuardsBeforeTouchingTheDatabase(t *testing.T) {
	r := newNilRepo() // a nil *gorm.DB: reaching the query at all would panic

	if err := r.Rename("", "id", "Ana"); !errors.Is(err, lead.ErrLeadWorkspaceRequired) {
		t.Errorf("empty workspace = %v, want ErrLeadWorkspaceRequired", err)
	}
	if err := r.Rename("ws", "  ", "Ana"); !errors.Is(err, lead.ErrLeadRequired) {
		t.Errorf("blank id = %v, want ErrLeadRequired", err)
	}
	if err := r.Rename("ws", "id", strings.Repeat("a", lead.MaxLeadNameLength+1)); !errors.Is(err, lead.ErrLeadNameTooLong) {
		t.Errorf("oversized name = %v, want ErrLeadNameTooLong", err)
	}
}

// The scoping assertion. An UPDATE keyed on id alone would let anyone who
// guessed a uuid rename a lead inside another workspace, and no amount of
// handler-level checking would catch it, because the handler already believes
// the repository is scoped.
func TestRename_UpdateIsScopedByWorkspaceNotJustID(t *testing.T) {
	db, mock, sqlDB := newRenameDB(t)
	defer sqlDB.Close()

	mock.ExpectExec(`UPDATE "leads" SET .*"name"=.*WHERE .*id = .* AND workspace_id = `).
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), "lead-1", "ws-1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := (&repository{db: db}).Rename("ws-1", "lead-1", "Ana Maria"); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// The name reaching the column is the NORMALIZED one, so what the operator
// sees after a reload is what was stored, not what they typed.
func TestRename_WritesTheNormalizedName(t *testing.T) {
	db, mock, sqlDB := newRenameDB(t)
	defer sqlDB.Close()

	mock.ExpectExec(`UPDATE "leads" SET`).
		WithArgs("Ana Maria", sqlmock.AnyArg(), "lead-1", "ws-1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := (&repository{db: db}).Rename("ws-1", "lead-1", "  Ana    Maria  "); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// Clearing is a real write of "", not a skipped one. This is the case Merge
// cannot express, and the reason Rename exists as its own port method.
func TestRename_ClearingWritesAnEmptyName(t *testing.T) {
	db, mock, sqlDB := newRenameDB(t)
	defer sqlDB.Close()

	mock.ExpectExec(`UPDATE "leads" SET`).
		WithArgs("", sqlmock.AnyArg(), "lead-1", "ws-1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := (&repository{db: db}).Rename("ws-1", "lead-1", "   "); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// Zero rows means the id does not exist IN THIS WORKSPACE. Reporting success
// would tell an operator their rename landed when nothing changed, and would
// also confirm to a prober that some other workspace's id is real.
func TestRename_NoRowsIsNotFound(t *testing.T) {
	db, mock, sqlDB := newRenameDB(t)
	defer sqlDB.Close()

	mock.ExpectExec(`UPDATE "leads" SET`).
		WillReturnResult(sqlmock.NewResult(0, 0))

	err := (&repository{db: db}).Rename("ws-1", "lead-1", "Ana")
	if !errors.Is(err, lead.ErrLeadNotFound) {
		t.Fatalf("err = %v, want ErrLeadNotFound", err)
	}
}
