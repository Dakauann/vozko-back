package metaplatform_repository

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	mp "vozko/domain/metaplatform"
)

func newMockDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock, *sql.DB) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatal(err)
	}
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB, PreferSimpleProtocol: true, WithoutReturning: true}),
		&gorm.Config{SkipDefaultTransaction: true})
	if err != nil {
		t.Fatal(err)
	}
	return db, mock, sqlDB
}

func TestCreateAndFinish(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	repo := NewDeletionRequestRepository(db)
	at := time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC)

	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "meta_data_deletion_requests"`)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE "meta_data_deletion_requests" SET`)).
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := repo.Create(context.Background(), &mp.DeletionRequest{Code: "c1", App: mp.AppMeta, AppScopedUserID: "u", Status: mp.DeletionReceived, RequestedAt: at}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Finish(context.Background(), "c1", mp.DeletionCompleted, "", at); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestFindMissingCode(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "meta_data_deletion_requests"`)).
		WillReturnRows(sqlmock.NewRows([]string{"code"}))
	if _, err := NewDeletionRequestRepository(db).FindByCode(context.Background(), "nope"); !errors.Is(err, mp.ErrRequestNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestFinishUnknownCode(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE "meta_data_deletion_requests" SET`)).
		WillReturnResult(sqlmock.NewResult(0, 0))
	if err := NewDeletionRequestRepository(db).Finish(context.Background(), "nope", mp.DeletionCompleted, "", time.Now()); !errors.Is(err, mp.ErrRequestNotFound) {
		t.Fatalf("got %v", err)
	}
}
