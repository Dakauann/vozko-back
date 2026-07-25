package workflow_repository

import (
	"database/sql"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func newResolverMockDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock, *sql.DB) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatal(err)
	}
	db, err := gorm.Open(postgres.New(postgres.Config{
		Conn:                 sqlDB,
		PreferSimpleProtocol: true,
		WithoutReturning:     true,
	}), &gorm.Config{SkipDefaultTransaction: true})
	if err != nil {
		t.Fatal(err)
	}
	return db, mock, sqlDB
}

// A resolvable phone returns the entry id joined through the workspace-scoped
// campaign, tagged as a whatsapp entry.
func TestResolveByPhone_Match(t *testing.T) {
	db, mock, sqlDB := newResolverMockDB(t)
	defer sqlDB.Close()
	repo := NewEntryResolverRepository(db)

	mock.ExpectQuery(`whatsapp_campaign_entries`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("entry-x"))

	id, typ, err := repo.ResolveByPhone("ws1", "+55 11 99888-7777")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "entry-x" || typ != "whatsapp" {
		t.Fatalf("expected entry-x/whatsapp, got %q/%q", id, typ)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sqlmock expectations: %v", err)
	}
}

// A phone with no matching entry resolves to empty, not an error.
func TestResolveByPhone_NoMatch(t *testing.T) {
	db, mock, sqlDB := newResolverMockDB(t)
	defer sqlDB.Close()
	repo := NewEntryResolverRepository(db)

	mock.ExpectQuery(`whatsapp_campaign_entries`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	id, typ, err := repo.ResolveByPhone("ws1", "+5511999999999")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "" || typ != "" {
		t.Fatalf("expected empty resolution, got %q/%q", id, typ)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sqlmock expectations: %v", err)
	}
}

// An unparseable phone short-circuits before touching the DB (no query expected).
func TestResolveByPhone_InvalidPhoneSkipsDB(t *testing.T) {
	db, mock, sqlDB := newResolverMockDB(t)
	defer sqlDB.Close()
	repo := NewEntryResolverRepository(db)

	id, typ, err := repo.ResolveByPhone("ws1", "not-a-phone")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "" || typ != "" {
		t.Fatalf("expected empty resolution for an invalid phone, got %q/%q", id, typ)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("no DB call expected for an invalid phone: %v", err)
	}
}

// An empty workspace never queries either.
func TestResolveByPhone_EmptyWorkspaceSkipsDB(t *testing.T) {
	db, mock, sqlDB := newResolverMockDB(t)
	defer sqlDB.Close()
	repo := NewEntryResolverRepository(db)

	id, _, err := repo.ResolveByPhone("", "+5511998887777")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "" {
		t.Fatalf("expected empty resolution for an empty workspace, got %q", id)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("no DB call expected for an empty workspace: %v", err)
	}
}
