package balance_repository

import (
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"vozko/domain/balance"
)

var capColumns = []string{"workspace_id", "monthly_limit", "cycle_day", "updated_by", "updated_at", "unlocked_by", "unlocked_at", "counted_from", "used"}

func TestMonthlySendCapUsageWithoutACapIsNil(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	mock.ExpectQuery(`SELECT \* FROM "workspace_monthly_send_caps" WHERE workspace_id = \$1 LIMIT \$2`).
		WithArgs("ws-1", 1).WillReturnRows(sqlmock.NewRows(capColumns))

	usage, err := NewMonthlySendCapRepository(db).MonthlySendCapUsage("ws-1", time.Now())
	if err != nil || usage != nil {
		t.Fatalf("usage = %+v, %v; want nil, nil", usage, err)
	}
}

func TestMonthlySendCapUsageReadsTheOpenCycle(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	at := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	current := balance.SendCapCycleStart(at, 15)
	mock.ExpectQuery(`FROM "workspace_monthly_send_caps"`).WithArgs("ws-1", 1).
		WillReturnRows(sqlmock.NewRows(capColumns).AddRow("ws-1", int64(40000), 15, "admin-1", at, nil, nil, current, int64(1122)))

	usage, err := NewMonthlySendCapRepository(db).MonthlySendCapUsage("ws-1", at)
	if err != nil {
		t.Fatalf("MonthlySendCapUsage: %v", err)
	}
	if usage == nil || usage.Used != 1122 || usage.Remaining() != 38878 || !usage.CycleStart.Equal(current) {
		t.Fatalf("usage = %+v", usage)
	}
}

func TestMonthlySendCapUsageCountsTheLedgerOfANeverCountedCap(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	at := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`FROM "workspace_monthly_send_caps"`).WithArgs("ws-1", 1).
		WillReturnRows(sqlmock.NewRows(capColumns).AddRow("ws-1", int64(100), 1, "admin-1", at, nil, nil, nil, int64(0)))
	mock.ExpectQuery(`SELECT COALESCE\(SUM`).WithArgs("ws-1", balance.SendCapCycleStart(at, 1)).
		WillReturnRows(sqlmock.NewRows([]string{"coalesce"}).AddRow(int64(7)))

	usage, err := NewMonthlySendCapRepository(db).MonthlySendCapUsage("ws-1", at)
	if err != nil || usage == nil || usage.Used != 7 {
		t.Fatalf("usage = %+v, %v", usage, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sqlmock expectations: %v", err)
	}
}

func TestMonthlySendCapUsageSurfacesAReadFailure(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	mock.ExpectQuery(`FROM "workspace_monthly_send_caps"`).WillReturnError(errors.New("db down"))

	if _, err := NewMonthlySendCapRepository(db).MonthlySendCapUsage("ws-1", time.Now()); err == nil {
		t.Fatal("a failed read must not read as no cap")
	}
}
