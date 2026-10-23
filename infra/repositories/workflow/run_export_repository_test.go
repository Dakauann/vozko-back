package workflow_repository

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"vozko/domain/workflow"
)

func exportFilterFor(workspaceID, workflowID string, from time.Time) workflow.RunExportFilter {
	return workflow.RunExportFilter{Runs: workflow.ListRunsInput{WorkspaceID: workspaceID, WorkflowID: workflowID}, From: from, To: from.Add(7 * 24 * time.Hour)}
}

func TestTheExportCountStopsAtTheCeilingInsteadOfCountingEveryRun(t *testing.T) {
	db, mock := mockDB(t)
	from := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	filter := exportFilterFor("ws-1", "wf-1", from)
	mock.ExpectQuery(sqlPattern(`SELECT count(*) FROM (SELECT 1 FROM "workflow_runs" WHERE workspace_id = ? AND workflow_id = ? AND started_at >= ? AND started_at < ? LIMIT ?) AS capped`)).
		WithArgs("ws-1", "wf-1", from, filter.To, 5001).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(5001))

	got, err := NewRunExportSource(db).CountForExportUpTo(context.Background(), filter, 5001)
	if err != nil || got != 5001 {
		t.Fatalf("count = %d err = %v", got, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestTheExportPagesByKeysetNewestFirstWithoutOffset(t *testing.T) {
	db, mock := mockDB(t)
	from := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	filter := exportFilterFor("ws-1", "wf-1", from)
	status := workflow.RunStatusError
	filter.Runs.Status, filter.Runs.EntryID = &status, "e-1"
	after := &workflow.RunCursor{StartedAt: from.Add(time.Hour), ID: "run-9"}
	mock.ExpectQuery(sqlPattern(`SELECT * FROM "workflow_runs" WHERE workspace_id = ? AND workflow_id = ? AND entry_id = ? AND status = ? AND started_at >= ? AND started_at < ? AND (started_at <= ? AND (started_at < ? OR id < ?)) ORDER BY started_at DESC, id DESC LIMIT ?`)).
		WithArgs("ws-1", "wf-1", "e-1", "error", from, filter.To, after.StartedAt, after.StartedAt, "run-9", 50).
		WillReturnRows(sqlmock.NewRows([]string{"id", "workflow_id", "state", "started_at"}).AddRow("run-8", "wf-1", `{"curso":"ADS"}`, from))

	page, err := NewRunExportSource(db).ExportPage(context.Background(), filter, after, 50)
	if err != nil || len(page) != 1 || page[0].ID != "run-8" || page[0].State.Vars["curso"] != "ADS" {
		t.Fatalf("page = %+v err = %v", page, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestTheFirstExportPageHasNoCursor(t *testing.T) {
	db, mock := mockDB(t)
	from := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	filter := exportFilterFor("ws-1", "wf-1", from)
	mock.ExpectQuery(sqlPattern(`SELECT * FROM "workflow_runs" WHERE workspace_id = ? AND workflow_id = ? AND started_at >= ? AND started_at < ? ORDER BY started_at DESC, id DESC LIMIT ?`)).
		WithArgs("ws-1", "wf-1", from, filter.To, 50).WillReturnRows(sqlmock.NewRows([]string{"id"}))

	if _, err := NewRunExportSource(db).ExportPage(context.Background(), filter, nil, 50); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestTheStepsOfAPageAreOneQueryInTheOrderOfTheRuns(t *testing.T) {
	if got := strings.Count(exportStepsSQL, "?"); got != 1 {
		t.Fatalf("placeholders = %d", got)
	}
	db, mock := mockDB(t)
	at := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	mock.ExpectQuery(sqlPattern(exportStepsSQL)).WithArgs(`["run-2","run-1","run-3"]`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "run_id", "node_id", "node_type", "status", "output", "executed_at"}).
			AddRow("s-1", "run-2", "n1", "action_http_request", "executed", `{"ok":true}`, at).
			AddRow("s-2", "run-2", "n2", "action_code", "failed", nil, at.Add(time.Second)).
			AddRow("s-3", "run-3", "n1", "action_http_request", "executed", nil, at))

	var visited []string
	var sizes []int
	err := NewRunStepsReader(db).EachRunSteps(context.Background(), []string{"run-2", "run-1", "run-3"}, func(runID string, steps []*workflow.WorkflowRunLog) error {
		visited, sizes = append(visited, runID), append(sizes, len(steps))
		if steps[0].RunID != runID {
			t.Fatalf("step of %s handed to %s", steps[0].RunID, runID)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(visited, ",") != "run-2,run-3" || sizes[0] != 2 || sizes[1] != 1 {
		t.Fatalf("visited %v sizes %v", visited, sizes)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
