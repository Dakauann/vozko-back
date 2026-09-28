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

	mock.ExpectExec(`INSERT INTO "workspace_monthly_send_caps" .*ON CONFLICT \("workspace_id"\) DO UPDATE SET "monthly_limit"="excluded"."monthly_limit","updated_by"="excluded"."updated_by","updated_at"="excluded"."updated_at","unlocked_by"="excluded"."unlocked_by","unlocked_at"="excluded"."unlocked_at"`).
		WillReturnResult(sqlmock.NewResult(1, 1))

	err := NewMonthlySendCapRepository(db).UpsertMonthlySendCap(balance.MonthlySendCap{
		WorkspaceID: "ws-1", Limit: 10, UpdatedBy: "admin-1", UpdatedAt: time.Now(),
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

	mock.ExpectExec(`DELETE FROM "workspace_monthly_send_caps" WHERE workspace_id = \$1`).
		WithArgs("ws-1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := NewMonthlySendCapRepository(db).DeleteMonthlySendCap("ws-1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sqlmock expectations: %v", err)
	}
}

func TestListMonthlySendCapUsage_CountsWithTheSharedNetRule(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	since := time.Date(2026, 9, 1, 3, 0, 0, 0, time.UTC)
	updatedAt := time.Date(2026, 9, 20, 10, 0, 0, 0, time.UTC)

	mock.ExpectQuery(`FROM workspace_monthly_send_caps c JOIN workspaces w ON w.id = c.workspace_id AND w.deleted_at IS NULL`).
		WithArgs(since).
		WillReturnRows(sqlmock.NewRows([]string{"workspace_id", "monthly_limit", "updated_by", "updated_at", "unlocked_by", "unlocked_at", "workspace_name", "used"}).
			AddRow("ws-1", int64(100), "admin-1", updatedAt, nil, nil, "Acme", int64(85)))

	rows, err := NewMonthlySendCapRepository(db).ListMonthlySendCapUsage(since)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 1 || rows[0].Cap.WorkspaceID != "ws-1" || rows[0].Cap.Limit != 100 || rows[0].WorkspaceName != "Acme" || rows[0].Used != 85 || rows[0].Cap.UnlockedBy != nil {
		t.Fatalf("unexpected rows %+v", rows)
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
