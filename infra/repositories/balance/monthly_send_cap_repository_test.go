package balance_repository

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"vozko/domain/balance"
)

func TestGetMonthlySendCap_MissingRowMeansNoCap(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()

	mock.ExpectQuery(`SELECT \* FROM "workspace_monthly_send_caps" WHERE workspace_id = \$1`).
		WithArgs("ws-1", 1).
		WillReturnRows(sqlmock.NewRows([]string{"workspace_id"}))

	cap, err := NewMonthlySendCapRepository(db).GetMonthlySendCap("ws-1")
	if err != nil || cap != nil {
		t.Fatalf("want no cap and no error, got %+v, %v", cap, err)
	}
}

func TestGetMonthlySendCap_ReadFailureIsReturned(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	boom := errors.New("connection reset")

	mock.ExpectQuery(`SELECT \* FROM "workspace_monthly_send_caps"`).WillReturnError(boom)

	cap, err := NewMonthlySendCapRepository(db).GetMonthlySendCap("ws-1")
	if !errors.Is(err, boom) || cap != nil {
		t.Fatalf("a read failure must surface, never read as no cap; got %+v, %v", cap, err)
	}
}

func TestGetMonthlySendCap_MapsRow(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	updatedAt := time.Date(2026, 9, 20, 10, 0, 0, 0, time.UTC)
	unlockedAt := time.Date(2026, 9, 21, 10, 0, 0, 0, time.UTC)

	mock.ExpectQuery(`SELECT \* FROM "workspace_monthly_send_caps"`).
		WillReturnRows(sqlmock.NewRows([]string{"workspace_id", "monthly_limit", "updated_by", "updated_at", "unlocked_by", "unlocked_at"}).
			AddRow("ws-1", int64(900), "admin-1", updatedAt, "root-1", unlockedAt))

	cap, err := NewMonthlySendCapRepository(db).GetMonthlySendCap("ws-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if cap.WorkspaceID != "ws-1" || cap.Limit != 900 || cap.UpdatedBy != "admin-1" || !cap.UpdatedAt.Equal(updatedAt) ||
		cap.UnlockedBy == nil || *cap.UnlockedBy != "root-1" || cap.UnlockedAt == nil || !cap.UnlockedAt.Equal(unlockedAt) {
		t.Errorf("unexpected cap %+v", cap)
	}
}

func TestUpsertMonthlySendCap_OverwritesEveryMutableColumn(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()

	mock.ExpectExec(`INSERT INTO "workspace_monthly_send_caps" .*ON CONFLICT \("workspace_id"\) DO UPDATE SET "monthly_limit"="excluded"."monthly_limit","cycle_day"="excluded"."cycle_day","updated_by"="excluded"."updated_by","updated_at"="excluded"."updated_at","unlocked_by"="excluded"."unlocked_by","unlocked_at"="excluded"."unlocked_at","counted_from"=CASE WHEN workspace_monthly_send_caps.cycle_day = excluded.cycle_day THEN workspace_monthly_send_caps.counted_from END`).
		WillReturnResult(sqlmock.NewResult(1, 1))

	err := NewMonthlySendCapRepository(db).UpsertMonthlySendCap(balance.MonthlySendCap{
		WorkspaceID: "ws-1", Limit: 10, CycleDay: 15, UpdatedBy: "admin-1", UpdatedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sqlmock expectations: %v", err)
	}
}

func TestDeleteMonthlySendCap(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()

	mock.ExpectBegin()
	mock.ExpectExec(`DELETE FROM "workspace_monthly_send_caps" WHERE workspace_id = \$1`).
		WithArgs("ws-1").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`DELETE FROM "workspace_monthly_send_slots" WHERE workspace_id = \$1`).
		WithArgs("ws-1").
		WillReturnResult(sqlmock.NewResult(0, 3))
	mock.ExpectCommit()

	if err := NewMonthlySendCapRepository(db).DeleteMonthlySendCap("ws-1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sqlmock expectations: %v", err)
	}
}

func TestListMonthlySendCapUsage_ReadsEachCapInItsOwnCycle(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	at := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	updatedAt := time.Date(2026, 9, 20, 10, 0, 0, 0, time.UTC)
	current := balance.SendCapCycleStart(at, 15)
	stale := balance.SendCapCycleStart(at, 1).AddDate(0, -1, 0)

	mock.ExpectQuery(`FROM workspace_monthly_send_caps c JOIN workspaces w ON w.id = c.workspace_id AND w.deleted_at IS NULL`).
		WillReturnRows(sqlmock.NewRows([]string{"workspace_id", "monthly_limit", "cycle_day", "updated_by", "updated_at", "unlocked_by", "unlocked_at", "counted_from", "used", "workspace_name"}).
			AddRow("ws-current", int64(100), 15, "admin-1", updatedAt, nil, nil, current, int64(85), "Current").
			AddRow("ws-stale", int64(100), 1, "admin-1", updatedAt, nil, nil, stale, int64(99), "Stale").
			AddRow("ws-fresh", int64(100), 1, "admin-1", updatedAt, nil, nil, nil, int64(0), "Fresh"))
	mock.ExpectQuery(`SELECT COALESCE\(SUM`).WithArgs("ws-fresh", balance.SendCapCycleStart(at, 1)).
		WillReturnRows(sqlmock.NewRows([]string{"coalesce"}).AddRow(int64(7)))

	rows, err := NewMonthlySendCapRepository(db).ListMonthlySendCapUsage(at)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	got := map[string]balance.SendCapUsage{}
	for _, r := range rows {
		got[r.WorkspaceName] = r
	}
	if u := got["Current"]; u.Used != 85 || u.Cap.CycleDay != 15 || !u.CycleStart.Equal(current) {
		t.Fatalf("a current count is read as is: %+v", u)
	}
	if u := got["Stale"]; u.Used != 0 {
		t.Fatalf("a count from an earlier cycle reads as zero: %+v", u)
	}
	if u := got["Fresh"]; u.Used != 7 {
		t.Fatalf("a cap never counted reads the ledger: %+v", u)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sqlmock expectations: %v", err)
	}
}

func TestNetTemplateSendsSinceSQL_SharesTheAggregateRule(t *testing.T) {
	sql := netTemplateSendsSinceSQL("c.workspace_id")
	for _, part := range []string{netTemplateSendExpr, templateSendRowsFilter, "bt.workspace_id = c.workspace_id", "bt.service_type = 'whatsapp_campaign'"} {
		if !strings.Contains(sql, part) {
			t.Errorf("count SQL is missing %q", part)
		}
	}
}
