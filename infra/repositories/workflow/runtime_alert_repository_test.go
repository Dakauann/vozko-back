package workflow_repository

import (
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"vozko/domain/conversation"
	"vozko/domain/shared"
	"vozko/domain/workflow"
)

var alertAt = time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)

func TestTheOpenAlertConflictTargetIsTheOpenUniqueIndex(t *testing.T) {
	target := "ON CONFLICT (workspace_id, workflow_id, node_id, agent_id, subject, kind) WHERE status = 'open'"
	for name, statement := range map[string]string{"record": recordRuntimeAlertSQL, "open": openRuntimeAlertsSuffix} {
		if !strings.Contains(statement, target) {
			t.Fatalf("%s must conflict on the partial unique index ux_wfra_open, or a second open row would be inserted:\n%s", name, statement)
		}
	}
	if got := strings.Count(recordRuntimeAlertSQL, "?"); got != 11 {
		t.Fatalf("record placeholders = %d, want 11", got)
	}
	if !strings.Contains(recordRuntimeAlertSQL, "count = workflow_runtime_alerts.count + EXCLUDED.count") {
		t.Fatal("an event hook must add to the open alert, not replace its count")
	}
}

func TestARecordedEventUpsertsOneRow(t *testing.T) {
	db, mock := mockDB(t)
	signal := workflow.StuckRunSignal(&workflow.WorkflowRun{ID: "run-1", WorkflowID: "wf-1", WorkspaceID: "ws-1"}, alertAt)
	mock.ExpectExec(sqlPattern(recordRuntimeAlertSQL)).
		WithArgs(sqlmock.AnyArg(), "ws-1", "wf-1", "", "", "", "run_stuck", 1, alertAt, alertAt, "run-1").
		WillReturnResult(sqlmock.NewResult(0, 1))
	if err := NewRuntimeAlertRepository(db).RecordRuntimeAlert(signal); err != nil {
		t.Fatal(err)
	}
	if err := NewRuntimeAlertRepository(db).RecordRuntimeAlert(workflow.RuntimeAlertSignal{}); err != nil {
		t.Fatalf("an invalid signal writes nothing: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestNewAndTouchedAlertsAreWrittenInOneStatementEach(t *testing.T) {
	db, mock := mockDB(t)
	key := workflow.RuntimeAlertKey{WorkspaceID: "ws-1", WorkflowID: "wf-1", NodeID: "n1", Kind: workflow.AlertNodeFailing}
	other := key
	other.NodeID = "n2"
	alerts := []workflow.RuntimeAlert{
		{ID: "a-1", Key: key, Count: 3, FirstSeen: alertAt, LastSeen: alertAt, Sample: "run-1"},
		{ID: "a-2", Key: other, Count: 4, FirstSeen: alertAt, LastSeen: alertAt, Sample: "run-2"},
	}
	open := openRuntimeAlertsPrefix + openRuntimeAlertRow + ", " + openRuntimeAlertRow + openRuntimeAlertsSuffix
	mock.ExpectExec(sqlPattern(open)).
		WithArgs(
			sqlmock.AnyArg(), "ws-1", "wf-1", "n1", "", "", "node_failing", 3, alertAt, alertAt, "run-1",
			sqlmock.AnyArg(), "ws-1", "wf-1", "n2", "", "", "node_failing", 4, alertAt, alertAt, "run-2",
		).WillReturnResult(sqlmock.NewResult(0, 2))
	touch := touchRuntimeAlertsPrefix + touchRuntimeAlertRow + ", " + touchRuntimeAlertRow + touchRuntimeAlertsSuffix
	mock.ExpectExec(sqlPattern(touch)).
		WithArgs("a-1", 3, alertAt, "run-1", "a-2", 4, alertAt, "run-2").
		WillReturnResult(sqlmock.NewResult(0, 2))

	repo := NewRuntimeAlertRepository(db)
	if err := repo.OpenAlerts(alerts); err != nil {
		t.Fatal(err)
	}
	if err := repo.TouchAlerts(alerts); err != nil {
		t.Fatal(err)
	}
	if err := repo.OpenAlerts(nil); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestResolvingAndNotifyingTouchOnlyOpenAlerts(t *testing.T) {
	for name, statement := range map[string]string{"resolve": resolveRuntimeAlertsSQL, "notify": markRuntimeAlertsNotifiedSQL, "touch": touchRuntimeAlertsSuffix} {
		if !strings.Contains(statement, "status = 'open'") {
			t.Fatalf("%s must skip resolved alerts:\n%s", name, statement)
		}
	}
	db, mock := mockDB(t)
	mock.ExpectExec(`^UPDATE workflow_runtime_alerts SET status = 'resolved', resolved_at = \$1, updated_at = now\(\)\s+WHERE id IN \(\$2,\$3\) AND status = 'open'$`).
		WithArgs(alertAt, "a-1", "a-2").WillReturnResult(sqlmock.NewResult(0, 2))
	if err := NewRuntimeAlertRepository(db).ResolveAlerts([]string{"a-1", "a-2"}, alertAt); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSignalQueriesReadOnlyTheirRecentWindow(t *testing.T) {
	cases := []struct {
		name         string
		sql          string
		placeholders int
		must         []string
	}{
		{"deliveries", rejectedDeliveriesSQL, 2, []string{"received_at >= ?", "outcome = 'rejected'", "GROUP BY workspace_id, workflow_id, reason", "ORDER BY last_seen DESC LIMIT ?"}},
		{"tool calls", failedToolCallsSQL, 2, []string{"created_at >= ?", "outcome = 'error'", "tool_name = 'http_request'", `tool_name LIKE 'run\_workflow\_%'`, "ORDER BY last_seen DESC LIMIT ?"}},
		{"failing nodes", failingNodesSQL, 5, []string{"l.executed_at >= ? AND l.status = 'failed'", "r.updated_at >= ?", "position <= ?", "PARTITION BY r.workflow_id, l.node_id", "ORDER BY last_seen DESC LIMIT ?"}},
		{"node successes", nodeSuccessesAfterLastFailureSQL, 4, []string{"r.workflow_id IN ?", "r.updated_at >= ?", "f.failed_at IS NULL OR s.executed_at > f.failed_at"}},
	}
	for _, tc := range cases {
		if got := strings.Count(tc.sql, "?"); got != tc.placeholders {
			t.Errorf("%s placeholders = %d, want %d", tc.name, got, tc.placeholders)
		}
		for _, fragment := range tc.must {
			if !strings.Contains(tc.sql, fragment) {
				t.Errorf("%s misses %q", tc.name, fragment)
			}
		}
	}
}

func TestSignalGroupsAreReadIntoTheDomainShapes(t *testing.T) {
	db, mock := mockDB(t)
	since := alertAt.Add(-time.Hour)
	columns := []string{"workspace_id", "workflow_id", "reason", "count", "first_seen", "last_seen", "sample"}
	mock.ExpectQuery(sqlPattern(rejectedDeliveriesSQL)).WithArgs(since, maxAlertSignalGroups).
		WillReturnRows(sqlmock.NewRows(columns).AddRow("ws-1", "wf-1", "unauthorized", 6, since, alertAt, "d-9"))
	mock.ExpectQuery(sqlPattern(failedToolCallsSQL)).WithArgs(since, maxAlertSignalGroups).
		WillReturnRows(sqlmock.NewRows([]string{"workspace_id", "agent_id", "tool_name", "error_code", "count", "first_seen", "last_seen", "sample"}).
			AddRow("ws-1", "ag-1", "run_workflow_wf-1", "workflow_not_active", 2, since, alertAt, "c-1"))
	mock.ExpectQuery(sqlPattern(failingNodesSQL)).WithArgs(since, since, since, 10, maxAlertSignalGroups).
		WillReturnRows(sqlmock.NewRows([]string{"workspace_id", "workflow_id", "node_id", "count", "first_seen", "last_seen", "sample"}).
			AddRow("ws-1", "wf-1", "n55", 3, since, alertAt, "run-7"))

	repo := NewRuntimeAlertRepository(db)
	deliveries, err := repo.RejectedDeliveries(since)
	if err != nil || len(deliveries) != 1 || deliveries[0].Reason != "unauthorized" || deliveries[0].Count != 6 || deliveries[0].Sample != "d-9" {
		t.Fatalf("deliveries = %+v, err = %v", deliveries, err)
	}
	calls, err := repo.FailedToolCalls(since)
	if err != nil || len(calls) != 1 || calls[0].ErrorCode != "workflow_not_active" || calls[0].AgentID != "ag-1" {
		t.Fatalf("calls = %+v, err = %v", calls, err)
	}
	nodes, err := repo.FailingNodes(since, 10)
	if err != nil || len(nodes) != 1 {
		t.Fatalf("nodes = %+v, err = %v", nodes, err)
	}
	if got := nodes[0]; got.Key.Kind != workflow.AlertNodeFailing || got.Key.NodeID != "n55" || got.Sample != "run-7" || !got.LastSeen.Equal(alertAt) {
		t.Fatalf("node signal = %+v", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestNodeSuccessesAreAskedForEveryProbeInOneQuery(t *testing.T) {
	db, mock := mockDB(t)
	since := alertAt.Add(-time.Hour)
	mock.ExpectQuery(`WHERE r\.workflow_id IN \(\$1\) AND r\.updated_at >= \$2\s+AND \(r\.workflow_id::text, l\.node_id\) IN \(\(\$3,\$4\),\(\$5,\$6\)\)\s+AND l\.executed_at >= \$7`).
		WithArgs("wf-1", since, "wf-1", "n1", "wf-1", "n2", since).
		WillReturnRows(sqlmock.NewRows([]string{"workflow_id", "node_id", "successes"}).AddRow("wf-1", "n1", 3))

	got, err := NewRuntimeAlertRepository(db).NodeSuccessesAfterLastFailure([]workflow.NodeAlertProbe{{WorkflowID: "wf-1", NodeID: "n1"}, {WorkflowID: "wf-1", NodeID: "n2"}}, since)
	if err != nil {
		t.Fatal(err)
	}
	if got[workflow.NodeAlertProbe{WorkflowID: "wf-1", NodeID: "n1"}] != 3 || got[workflow.NodeAlertProbe{WorkflowID: "wf-1", NodeID: "n2"}] != 0 {
		t.Fatalf("successes = %+v", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestTheWorkspaceListKeepsDepartmentScopedPeopleToTheirWorkflows(t *testing.T) {
	db, mock := mockDB(t)
	mock.ExpectQuery(`^SELECT COUNT\(\*\) FROM workflow_runtime_alerts a\s+WHERE a\.workspace_id = \$1 AND a\.status = 'open'\s+AND EXISTS \(SELECT 1 FROM workflows w WHERE w\.id = CAST\(NULLIF\(a\.workflow_id, ''\) AS uuid\) AND w\.department_id IN \(\$2\)\)$`).
		WithArgs("ws-1", "dep-1").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	mock.ExpectQuery(`^SELECT a\.id, a\.workspace_id, .+ ORDER BY a\.last_seen DESC LIMIT \$3 OFFSET \$4$`).
		WithArgs("ws-1", "dep-1", 20, 0).
		WillReturnRows(sqlmock.NewRows([]string{"id", "workspace_id", "workflow_id", "node_id", "agent_id", "subject", "kind", "count", "first_seen", "last_seen", "sample", "status", "resolved_at", "notified_at"}).
			AddRow("a-1", "ws-1", "wf-1", "n1", "", "", "node_failing", 3, alertAt, alertAt, "run-1", "open", nil, nil))

	page, err := NewRuntimeAlertRepository(db).ListOpen("ws-1", workflow.DepartmentRestriction{Restricted: true, DepartmentIDs: []string{"dep-1"}}, conversation.VisibilityScope{}, shared.Pagination{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatal(err)
	}
	if page.TotalItems != 1 || len(page.Items) != 1 || page.Items[0].FixAction != workflow.FixOpenRun || page.Items[0].Severity != workflow.RuntimeAlertHigh {
		t.Fatalf("page = %+v", page)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestARestrictionWithNoDepartmentsListsNothingWithoutQuerying(t *testing.T) {
	db, mock := mockDB(t)
	page, err := NewRuntimeAlertRepository(db).ListOpen("ws-1", workflow.DepartmentRestriction{Restricted: true}, conversation.VisibilityScope{}, shared.Pagination{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatal(err)
	}
	if page.TotalItems != 0 || len(page.Items) != 0 {
		t.Fatalf("page = %+v, an empty department list must never read as the whole workspace", page)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestResolvedAlertsArePrunedInBoundedBatches(t *testing.T) {
	if got := strings.Count(deleteResolvedRuntimeAlertsSQL, "?"); got != 2 || !strings.Contains(deleteResolvedRuntimeAlertsSQL, "LIMIT ?") || !strings.Contains(deleteResolvedRuntimeAlertsSQL, "status = 'resolved'") {
		t.Fatalf("prune must delete only resolved alerts in bounded batches:\n%s", deleteResolvedRuntimeAlertsSQL)
	}
	db, mock := mockDB(t)
	mock.ExpectExec(sqlPattern(deleteResolvedRuntimeAlertsSQL)).WithArgs(alertAt, 5000).WillReturnResult(sqlmock.NewResult(0, 12))
	n, err := NewRuntimeAlertRepository(db).DeleteResolvedBefore(alertAt, 5000)
	if err != nil || n != 12 {
		t.Fatalf("n = %d, err = %v", n, err)
	}
}
