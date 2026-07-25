package balance_repository

import (
	"database/sql"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"vozko/domain/balance"
)

func newMockDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock, *sql.DB) {
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

func TestDebitBalance_PersistsIsRefund(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()

	repo := NewRepository(db)

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT .* FROM "balances"`).
		WithArgs("ws-1", sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id", "workspace_id", "amount", "currency"}).
			AddRow("bal-1", "ws-1", int64(10_000_000), "USD"))
	mock.ExpectExec(`INSERT INTO "balance_transactions".*"is_refund"`).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(`UPDATE "balances"`).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	tx, err := repo.DebitBalance(balance.DebitBalanceInput{
		WorkspaceID:        "ws-1",
		Amount:             5_000_000,
		ServiceType:        balance.ServiceTopUp,
		IsRefund:           true,
		ExchangeRateMicros: 6_000_000,
	})
	if err != nil {
		t.Fatalf("DebitBalance error: %v", err)
	}
	if tx == nil || !tx.IsRefund {
		t.Fatalf("expected returned transaction with IsRefund=true, got %+v", tx)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sqlmock expectations: %v", err)
	}
}

func TestDebitBalance_DefaultsIsRefundFalse(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()

	repo := NewRepository(db)

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT .* FROM "balances"`).
		WithArgs("ws-1", sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id", "workspace_id", "amount", "currency"}).
			AddRow("bal-1", "ws-1", int64(10_000_000), "USD"))
	mock.ExpectExec(`INSERT INTO "balance_transactions"`).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(`UPDATE "balances"`).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	tx, err := repo.DebitBalance(balance.DebitBalanceInput{
		WorkspaceID:        "ws-1",
		Amount:             1_000_000,
		ServiceType:        balance.ServiceWhatsAppCampaign,
		ExchangeRateMicros: 6_000_000,
	})
	if err != nil {
		t.Fatalf("DebitBalance error: %v", err)
	}
	if tx == nil || tx.IsRefund {
		t.Fatalf("expected returned transaction with IsRefund=false, got %+v", tx)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sqlmock expectations: %v", err)
	}
}
