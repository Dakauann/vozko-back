package workflow_repository

import (
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestRetentionStatementsAreBoundedAndTouchOnlyFinishedRuns(t *testing.T) {
	cutoff := time.Date(2026, 4, 22, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		sql  string
		run  func(r *traceRetentionRepository) (int64, error)
		must []string
	}{
		{
			name: "payloads",
			sql:  pruneStepPayloadsSQL,
			run:  func(r *traceRetentionRepository) (int64, error) { return r.PruneStepPayloads(cutoff, 5000) },
			must: []string{"SET input = NULL, output = NULL, http = NULL, script_logs = NULL, stack = NULL", "executed_at < ?", "LIMIT ?"},
		},
		{
			name: "trigger data",
			sql:  pruneTriggerDataSQL,
			run:  func(r *traceRetentionRepository) (int64, error) { return r.PruneTriggerData(cutoff, 5000) },
			must: []string{"SET trigger_data = NULL", "trigger_data IS NOT NULL AND started_at < ?", "LIMIT ?"},
		},
		{
			name: "steps",
			sql:  deleteStepsOfFinishedRunsSQL,
			run:  func(r *traceRetentionRepository) (int64, error) { return r.DeleteStepsOfFinishedRuns(cutoff, 5000) },
			must: []string{"r.status IN ('completed', 'error', 'cancelled')", "r.started_at < ?", "LIMIT ?"},
		},
		{
			name: "runs",
			sql:  deleteFinishedRunsSQL,
			run:  func(r *traceRetentionRepository) (int64, error) { return r.DeleteFinishedRuns(cutoff, 5000) },
			must: []string{"r.status IN ('completed', 'error', 'cancelled')", "NOT EXISTS (SELECT 1 FROM workflow_run_logs l WHERE l.run_id = r.id)", "LIMIT ?"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := strings.Count(tc.sql, "?"); got != 2 {
				t.Fatalf("placeholders = %d, want 2", got)
			}
			for _, fragment := range tc.must {
				if !strings.Contains(tc.sql, fragment) {
					t.Fatalf("statement misses %q:\n%s", fragment, tc.sql)
				}
			}
			if strings.Contains(tc.sql, "'running'") || strings.Contains(tc.sql, "'waiting'") {
				t.Fatal("retention must never touch a live run")
			}
			db, mock := mockDB(t)
			mock.ExpectExec(sqlPattern(tc.sql)).WithArgs(cutoff, 5000).WillReturnResult(sqlmock.NewResult(0, 5000))
			affected, err := tc.run(NewTraceRetentionRepository(db).(*traceRetentionRepository))
			if err != nil || affected != 5000 {
				t.Fatalf("affected = %d, err = %v", affected, err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestVersionPruningKeepsTheLatestPublishedAndEveryPinnedVersion(t *testing.T) {
	for _, fragment := range []string{
		"w.id IS NULL OR w.deleted_at IS NOT NULL",
		"HAVING COUNT(*) > ?",
		"v.version IS DISTINCT FROM w.published_version",
		"ORDER BY k.version DESC",
		"OFFSET ? LIMIT 1",
		"r.workflow_version = v.version",
		"r.status IN ('running', 'waiting')",
		"LIMIT ?",
	} {
		if !strings.Contains(pruneWorkflowVersionsSQL, fragment) {
			t.Fatalf("version pruning misses %q:\n%s", fragment, pruneWorkflowVersionsSQL)
		}
	}
	if got := strings.Count(pruneWorkflowVersionsSQL, "?"); got != 3 {
		t.Fatalf("placeholders = %d, want 3", got)
	}
	db, mock := mockDB(t)
	mock.ExpectExec(sqlPattern(pruneWorkflowVersionsSQL)).WithArgs(50, 50, 5000).WillReturnResult(sqlmock.NewResult(0, 7))
	affected, err := NewTraceRetentionRepository(db).PruneVersions(50, 5000)
	if err != nil || affected != 7 {
		t.Fatalf("affected = %d, err = %v", affected, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
