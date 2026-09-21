package scheduled_message_repository

import (
	"database/sql"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	sm "vozko/domain/scheduled_message"
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
	}), &gorm.Config{SkipDefaultTransaction: true})
	if err != nil {
		t.Fatal(err)
	}
	return db, mock, sqlDB
}

var claimedAt = time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)

func TestClaimForDispatchIsOneGuardedUpdate(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()

	mock.ExpectQuery(regexp.QuoteMeta(`UPDATE scheduled_messages`)).
		WithArgs(string(sm.StatusSending), claimedAt, claimedAt, "sched-1", string(sm.StatusPending)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "status", "entry_type"}).
			AddRow("sched-1", string(sm.StatusSending), "whatsapp"))

	got, err := NewRepository(db).ClaimForDispatch("sched-1", claimedAt)
	if err != nil {
		t.Fatalf("ClaimForDispatch: %v", err)
	}
	if got == nil || got.ID != "sched-1" || got.Status != sm.StatusSending {
		t.Fatalf("claimed = %+v", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestClaimForDispatchLosingTheRaceIsNotAnError(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()

	mock.ExpectQuery(regexp.QuoteMeta(`UPDATE scheduled_messages`)).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	got, err := NewRepository(db).ClaimForDispatch("sched-1", claimedAt)
	if err != nil {
		t.Fatalf("a lost claim must not be an error, got %v", err)
	}
	if got != nil {
		t.Fatalf("a lost claim must return nothing, got %+v", got)
	}
}

func TestClaimDueBatchSkipsLockedRows(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()

	mock.ExpectQuery(`FOR UPDATE SKIP LOCKED`).
		WithArgs(string(sm.StatusSending), claimedAt, claimedAt, string(sm.StatusPending), claimedAt, 2).
		WillReturnRows(sqlmock.NewRows([]string{"id", "status"}).
			AddRow("sched-1", string(sm.StatusSending)).
			AddRow("sched-2", string(sm.StatusSending)))

	got, err := NewRepository(db).ClaimDueBatch(claimedAt, 2)
	if err != nil {
		t.Fatalf("ClaimDueBatch: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("claimed %d rows, want 2", len(got))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestCancelIsGuardedOnPending(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE "scheduled_messages" SET`)).
		WillReturnResult(sqlmock.NewResult(0, 0))

	err := NewRepository(db).Cancel("sched-1")
	if !errors.Is(err, sm.ErrNotPending) {
		t.Fatalf("err = %v, want ErrNotPending when the row already left pending", err)
	}
}

func TestMarkSentRequiresTheRowToBeInFlight(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE "scheduled_messages" SET`)).
		WillReturnResult(sqlmock.NewResult(0, 0))

	err := NewRepository(db).MarkSent("sched-1", "msg-1", claimedAt)
	if !errors.Is(err, sm.ErrNotPending) {
		t.Fatalf("err = %v, want a refusal to mark a row sent that was never claimed", err)
	}
}

func TestMarkSentSucceedsForAClaimedRow(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE "scheduled_messages" SET`)).
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := NewRepository(db).MarkSent("sched-1", "msg-1", claimedAt); err != nil {
		t.Fatalf("MarkSent: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestMarkFailedAcceptsBothPreDispatchStates(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE "scheduled_messages" SET`)).
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := NewRepository(db).MarkFailed("sched-1", sm.ReasonDispatchInterrupted, "unconfirmed")
	if err != nil {
		t.Fatalf("MarkFailed: %v", err)
	}
}

func TestPurgeNeverTouchesPendingRows(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()

	mock.ExpectExec(`DELETE FROM "scheduled_messages"`).
		WithArgs(string(sm.StatusSent), string(sm.StatusFailed), string(sm.StatusCanceled), claimedAt).
		WillReturnResult(sqlmock.NewResult(0, 7))

	n, err := NewRepository(db).PurgeTerminalBefore(claimedAt)
	if err != nil {
		t.Fatalf("PurgeTerminalBefore: %v", err)
	}
	if n != 7 {
		t.Fatalf("purged %d, want 7", n)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestFindByIdempotencyKeyIsWorkspaceScoped(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "scheduled_messages"`)).
		WithArgs("ws-1", "key-1", 1).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("sched-1"))

	got, err := NewRepository(db).FindByIdempotencyKey("ws-1", "key-1")
	if err != nil {
		t.Fatalf("FindByIdempotencyKey: %v", err)
	}
	if got.ID != "sched-1" {
		t.Fatalf("got %+v", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestFindByIdempotencyKeyRejectsAnEmptyKeyWithoutQuerying(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()

	_, err := NewRepository(db).FindByIdempotencyKey("ws-1", "   ")
	if !errors.Is(err, sm.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("an empty key still hit the database: %v", err)
	}
}
