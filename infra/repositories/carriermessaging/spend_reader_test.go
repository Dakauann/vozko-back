package carriermessaging_repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestSpendSinceSumsCarrierDebitsMinusRefunds(t *testing.T) {
	db, mock := newMockDB(t)
	since := testNow.Add(-30 * 24 * time.Hour)
	mock.ExpectQuery(`SELECT COALESCE\(SUM\(CASE WHEN type = 'debit' THEN amount ELSE -amount END\), 0\) FROM balance_transactions WHERE workspace_id = \$1 AND service_type IN \(\$2,\$3\) AND created_at >= \$4 AND \(\(type = 'debit' AND is_refund = false\) OR \(type = 'credit' AND is_refund = true\)\)`).
		WithArgs("ws1", "rcs", "sms", since).
		WillReturnRows(sqlmock.NewRows([]string{"coalesce"}).AddRow(int64(1500)))
	got, err := NewSpendReader(db).SpendSince(context.Background(), "ws1", since)
	if err != nil || got != 1500 {
		t.Fatalf("got %d %v", got, err)
	}
	expectationsMet(t, mock)
}

func TestSpendSinceReturnsErrors(t *testing.T) {
	db, mock := newMockDB(t)
	boom := errors.New("boom")
	mock.ExpectQuery(`SELECT`).WillReturnError(boom)
	if _, err := NewSpendReader(db).SpendSince(context.Background(), "ws1", testNow); !errors.Is(err, boom) {
		t.Fatalf("got %v", err)
	}
}
