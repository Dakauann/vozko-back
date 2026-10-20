package entrypurge_repository

import (
	"strings"
	"testing"
)

func TestEveryPurgeStatementBindsTheTypeAndTheIDs(t *testing.T) {
	for _, tbl := range append(append([]keyedTable{}, entryTables...), contactTables...) {
		for _, stmt := range tbl.statements() {
			if got := strings.Count(stmt, "?"); got != 2 {
				t.Errorf("%s binds %d placeholders, want 2: %s", tbl.table, got, stmt)
			}
		}
	}
	if got := strings.Count(mediaURLsSQL, "?"); got != 2 {
		t.Errorf("media urls bind %d placeholders: %s", got, mediaURLsSQL)
	}
}

func TestDependentRowsGoBeforeTheirParent(t *testing.T) {
	for _, tbl := range entryTables {
		if tbl.table != "workflow_runs" {
			continue
		}
		stmts := tbl.statements()
		if len(stmts) != 2 || !strings.Contains(stmts[0], "workflow_run_logs") || !strings.Contains(stmts[1], "DELETE FROM workflow_runs") {
			t.Fatalf("workflow run logs must be deleted before their runs: %v", stmts)
		}
		return
	}
	t.Fatal("workflow_runs is not purged")
}
