package billing_repository

import (
	"database/sql"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func newMockDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock, *sql.DB) {
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

func TestMarkProcessed_FirstTime(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	repo := NewProcessedEventRepository(db)

	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "processed_webhook_events"`)).
		WillReturnResult(sqlmock.NewResult(0, 1))

	first, err := repo.MarkProcessed("asaas", "pay_123:PAYMENT_RECEIVED", time.Unix(1000, 0))
	if err != nil {
		t.Fatalf("MarkProcessed error: %v", err)
	}
	if !first {
		t.Fatal("expected firstTime=true on a fresh insert")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sqlmock expectations: %v", err)
	}
}

func TestMarkProcessed_Duplicate(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	repo := NewProcessedEventRepository(db)

	// ON CONFLICT DO NOTHING inserts zero rows for a redelivery.
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "processed_webhook_events"`)).
		WillReturnResult(sqlmock.NewResult(0, 0))

	first, err := repo.MarkProcessed("asaas", "pay_123:PAYMENT_RECEIVED", time.Unix(1000, 0))
	if err != nil {
		t.Fatalf("MarkProcessed error: %v", err)
	}
	if first {
		t.Fatal("expected firstTime=false on a duplicate event")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sqlmock expectations: %v", err)
	}
}

func TestMarkProcessed_DBErrorPropagates(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	repo := NewProcessedEventRepository(db)

	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "processed_webhook_events"`)).
		WillReturnError(sql.ErrConnDone)

	if _, err := repo.MarkProcessed("asaas", "x", time.Unix(1, 0)); err == nil {
		t.Fatal("expected the DB error to propagate (fail closed, never silently dedup)")
	}
}
