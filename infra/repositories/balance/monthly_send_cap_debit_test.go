package balance_repository

import (
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"vozko/domain/balance"
)

func expectLockedBalance(mock sqlmock.Sqlmock, amount int64) {
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT .* FROM "balances"`).
		WithArgs("ws-1", sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id", "workspace_id", "amount", "currency"}).
			AddRow("bal-1", "ws-1", amount, "USD"))
}

func cappedDebit(limit int64, since time.Time) balance.DebitBalanceInput {
	return balance.DebitBalanceInput{
		WorkspaceID:        "ws-1",
		Amount:             1_000_000,
		ServiceType:        balance.ServiceWhatsAppCampaign,
		ExchangeRateMicros: 6_000_000,
		MonthlyCap:         &balance.MonthlySendCapGuard{Limit: limit, Since: since},
	}
}

const netTemplateSendsQuery = `SELECT COALESCE\(SUM\(CASE WHEN bt.type = 'debit' AND bt.is_refund = false THEN 1 WHEN bt.type = 'credit' AND bt.is_refund = true THEN -1 ELSE 0 END\), 0\)::bigint FROM balance_transactions bt WHERE bt.workspace_id = \$1 AND bt.service_type = 'whatsapp_campaign' AND bt.created_at >= \$2`

func TestDebitBalance_MonthlyCapAdmitsSendUnderTheLimit(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	since := time.Date(2026, 9, 1, 3, 0, 0, 0, time.UTC)

	expectLockedBalance(mock, 10_000_000)
	mock.ExpectQuery(netTemplateSendsQuery).
		WithArgs("ws-1", since).
		WillReturnRows(sqlmock.NewRows([]string{"coalesce"}).AddRow(int64(4)))
	mock.ExpectExec(`INSERT INTO "balance_transactions"`).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(`UPDATE "balances"`).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	if _, err := NewRepository(db).DebitBalance(cappedDebit(5, since)); err != nil {
		t.Fatalf("DebitBalance error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sqlmock expectations: %v", err)
	}
}

func TestDebitBalance_MonthlyCapRefusesWithoutWriting(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	since := time.Date(2026, 9, 1, 3, 0, 0, 0, time.UTC)

	expectLockedBalance(mock, 10_000_000)
	mock.ExpectQuery(netTemplateSendsQuery).
		WithArgs("ws-1", since).
		WillReturnRows(sqlmock.NewRows([]string{"coalesce"}).AddRow(int64(5)))
	mock.ExpectRollback()

	_, err := NewRepository(db).DebitBalance(cappedDebit(5, since))
	if !errors.Is(err, balance.ErrMonthlySendCapReached) {
		t.Fatalf("want ErrMonthlySendCapReached, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sqlmock expectations: %v", err)
	}
}

func TestDebitBalance_MonthlyCapWinsOverInsufficientBalance(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	since := time.Date(2026, 9, 1, 3, 0, 0, 0, time.UTC)

	expectLockedBalance(mock, 0)
	mock.ExpectQuery(netTemplateSendsQuery).
		WithArgs("ws-1", since).
		WillReturnRows(sqlmock.NewRows([]string{"coalesce"}).AddRow(int64(9)))
	mock.ExpectRollback()

	_, err := NewRepository(db).DebitBalance(cappedDebit(5, since))
	if !errors.Is(err, balance.ErrMonthlySendCapReached) {
		t.Fatalf("a capped workspace must be told about the cap, not the balance; got %v", err)
	}
}

func TestDebitBalance_MonthlyCapCountFailureAbortsTheDebit(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	since := time.Date(2026, 9, 1, 3, 0, 0, 0, time.UTC)
	boom := errors.New("statement timeout")

	expectLockedBalance(mock, 10_000_000)
	mock.ExpectQuery(netTemplateSendsQuery).WithArgs("ws-1", since).WillReturnError(boom)
	mock.ExpectRollback()

	tx, err := NewRepository(db).DebitBalance(cappedDebit(5, since))
	if !errors.Is(err, boom) || tx != nil {
		t.Fatalf("want the count error and no transaction, got %v, %+v", err, tx)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sqlmock expectations: %v", err)
	}
}
