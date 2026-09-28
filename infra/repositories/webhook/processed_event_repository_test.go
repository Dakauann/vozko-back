package webhook_repository

import (
	"context"
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

func TestClaimFirstTimeAndDuplicate(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	repo := NewProcessedEventRepository(db)

	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "webhook_processed_events"`)).
		WithArgs("fb:1:messages:m", "facebook", "acc", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "webhook_processed_events"`)).
		WillReturnResult(sqlmock.NewResult(0, 0))

	first, err := repo.Claim(context.Background(), "fb:1:messages:m", "facebook", "acc")
	if err != nil || !first {
		t.Fatalf("first claim = %t, %v", first, err)
	}
	again, err := repo.Claim(context.Background(), "fb:1:messages:m", "facebook", "acc")
	if err != nil || again {
		t.Fatalf("second claim = %t, %v", again, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestPurgeRemovesEveryChannelPastRetention(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	repo := NewProcessedEventRepository(db)
	cutoff := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)

	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM "webhook_processed_events" WHERE created_at < $1`)).
		WithArgs(cutoff).
		WillReturnResult(sqlmock.NewResult(0, 7))

	n, err := repo.PurgeOlderThan(context.Background(), cutoff)
	if err != nil || n != 7 {
		t.Fatalf("purged %d, %v", n, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
