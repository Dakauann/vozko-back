package database

import (
	"errors"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestConcurrentIndexBuildsWhenNothingIsThere(t *testing.T) {
	db, mock, sqlDB := sqlmockDB(t)
	defer sqlDB.Close()

	mock.ExpectQuery(`NOT i\.indisvalid`).WithArgs("idx_x").
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectExec(`CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_x`).
		WillReturnResult(sqlmock.NewResult(0, 0))

	if err := createIndexConcurrently(db, ddl{name: "idx_x", sql: "CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_x ON t (a)"}); err != nil {
		t.Fatalf("createIndexConcurrently() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestConcurrentIndexReplacesAnInvalidLeftover(t *testing.T) {
	db, mock, sqlDB := sqlmockDB(t)
	defer sqlDB.Close()

	mock.ExpectQuery(`NOT i\.indisvalid`).WithArgs("idx_x").
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectExec(`DROP INDEX CONCURRENTLY IF EXISTS idx_x`).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_x`).
		WillReturnResult(sqlmock.NewResult(0, 0))

	if err := createIndexConcurrently(db, ddl{name: "idx_x", sql: "CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_x ON t (a)"}); err != nil {
		t.Fatalf("createIndexConcurrently() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestConcurrentIndexReportsAFailedValidityCheck(t *testing.T) {
	db, mock, sqlDB := sqlmockDB(t)
	defer sqlDB.Close()

	mock.ExpectQuery(`NOT i\.indisvalid`).WillReturnError(errors.New("catalog unavailable"))

	if err := createIndexConcurrently(db, ddl{name: "idx_x", sql: "CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_x ON t (a)"}); err == nil {
		t.Fatalf("createIndexConcurrently() built an index without knowing whether an invalid one was in the way")
	}
}

func TestTheIndexesOnTheHotWorkflowRunTablesAreBuiltConcurrently(t *testing.T) {
	for _, name := range []string{
		"idx_wfrl_payload_prune", "idx_wfr_entry_status", "idx_wfr_workflow_started", "idx_wfr_terminal_started",
		"idx_wfr_agent_entry_started", "idx_wfr_entry_started", "idx_wfrl_failed_executed", "idx_wfr_workflow_updated",
		"idx_wfr_agent_awaiting_continuation", WorkflowRunTriggerDataPruneIndex, WorkflowRunTriggerInputIndex,
	} {
		if _, ok := ConcurrentIndexSQL(name); !ok {
			t.Errorf("%s is not in the concurrent index list; a plain CREATE INDEX locks workflow_runs or workflow_run_logs for writes", name)
		}
		if _, ok := PerformanceIndexSQL(name); ok {
			t.Errorf("%s is still declared as a plain index", name)
		}
	}
}

func TestEveryConcurrentIndexIsBuiltConcurrently(t *testing.T) {
	for _, idx := range concurrentIndexes() {
		sql := strings.Join(strings.Fields(idx.sql), " ")
		plain := strings.Contains(sql, "CREATE INDEX CONCURRENTLY IF NOT EXISTS "+idx.name+" ON ")
		unique := strings.Contains(sql, "CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS "+idx.name+" ON ")
		if !plain && !unique {
			t.Fatalf("%s is not built concurrently under its own name: %s", idx.name, idx.sql)
		}
	}
}

func TestTheLiveCampaignCountsReadOnlyTheirIndex(t *testing.T) {
	for _, name := range []string{CampaignEntryLiveStatusIndex, UnofficialCampaignEntryLiveStatusIndex} {
		sql, ok := ConcurrentIndexSQL(name)
		if !ok {
			t.Fatalf("%s is not in the concurrent index list; the live progress count would read every entry row", name)
		}
		flat := strings.Join(strings.Fields(sql), " ")
		if !strings.Contains(flat, "(campaign_id, status)") || !strings.Contains(flat, "WHERE deleted_at IS NULL") {
			t.Fatalf("%s must cover campaign_id and status for live entries only: %s", name, flat)
		}
	}
}
