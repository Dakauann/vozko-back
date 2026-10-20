package workflow_repository

import (
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"vozko/domain/workflow"
)

func TestTheAgentRunFilterMatchesWhatTheDomainWritesAsStarter(t *testing.T) {
	want := "started_by LIKE '" + workflow.StartedByAgent("") + "%'"
	for name, statement := range map[string]string{"latest": latestAgentCallRunSQL, "cancel": cancelActiveAgentRunsSQL} {
		if !strings.Contains(statement, want) {
			t.Fatalf("%s statement misses %q:\n%s", name, want, statement)
		}
	}
}

func TestTheLatestAgentRunIsOneBoundedQueryInsideTheCurrentConversationPeriod(t *testing.T) {
	if got := strings.Count(latestAgentCallRunSQL, "?"); got != 3 {
		t.Fatalf("placeholders = %d, want 3", got)
	}
	for _, fragment := range []string{
		"r.workspace_id = ? AND r.entry_id = ? AND r.entry_type = ?",
		"JOIN workflows w ON w.id = r.workflow_id",
		"LEFT JOIN workflow_versions v ON v.workflow_id = r.workflow_id AND v.version = COALESCE(r.workflow_version, w.published_version)",
		"CASE WHEN jsonb_typeof(" + runGraphSQL + "->'nodes') = 'array' THEN",
		"jsonb_array_elements(" + runGraphSQL + "->'nodes')",
		"e.event_type = 'finished' AND e.created_at > r.started_at",
		"m.sender_kind = 'workflow' AND m.sender_id = 'workflow:' || r.workflow_id::text",
		"ORDER BY r.started_at DESC",
		"LIMIT 1",
	} {
		if !strings.Contains(latestAgentCallRunSQL, fragment) {
			t.Fatalf("statement misses %q:\n%s", fragment, latestAgentCallRunSQL)
		}
	}

	db, mock := mockDB(t)
	started := time.Date(2026, 10, 19, 20, 15, 0, 0, time.UTC)
	finished := started.Add(time.Minute)
	wake := int64(1760900000000)
	mock.ExpectQuery(sqlPattern(latestAgentCallRunSQL)).WithArgs("ws-1", "e-1", "whatsapp").WillReturnRows(
		sqlmock.NewRows([]string{"id", "workflow_id", "workflow_name", "status", "current_node_id", "current_node_type", "wait_reason", "wake_at", "agent_result", "error", "started_at", "completed_at", "last_question"}).
			AddRow("r-1", "wf-1", "Matrícula", "waiting", "n7", "wait_for_reply", "reply", wake, `{"status":"ok"}`, "", started, finished, "Qual o seu CPF?"),
	)

	got, err := NewAgentCallRunsRepository(db).LatestForEntry("ws-1", "e-1", "whatsapp")
	if err != nil {
		t.Fatal(err)
	}
	if got.RunID != "r-1" || got.WorkflowID != "wf-1" || got.WorkflowName != "Matrícula" || got.Status != workflow.RunStatusWaiting {
		t.Fatalf("run = %+v", got)
	}
	if got.CurrentNodeID != "n7" || got.CurrentNodeType != workflow.NodeTypeWaitForReply || got.WaitReason != workflow.WaitReasonReply || got.WakeAt == nil || *got.WakeAt != wake {
		t.Fatalf("position = %+v", got)
	}
	if got.AgentResult["status"] != "ok" || got.LastQuestion != "Qual o seu CPF?" || !got.StartedAt.Equal(started) || got.FinishedAt == nil || !got.FinishedAt.Equal(finished) {
		t.Fatalf("details = %+v", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestNoAgentRunInTheCurrentPeriodReadsAsNone(t *testing.T) {
	db, mock := mockDB(t)
	mock.ExpectQuery(sqlPattern(latestAgentCallRunSQL)).WithArgs("ws-1", "e-1", "whatsapp").WillReturnRows(
		sqlmock.NewRows([]string{"id"}),
	)
	got, err := NewAgentCallRunsRepository(db).LatestForEntry("ws-1", "e-1", "whatsapp")
	if err != nil || got != nil {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestFinishingCancelsOnlyTheEntrysLiveAgentRunsInOneStatement(t *testing.T) {
	if got := strings.Count(cancelActiveAgentRunsSQL, "?"); got != 3 {
		t.Fatalf("placeholders = %d, want 3", got)
	}
	for _, fragment := range []string{"SET status = 'cancelled', error = ?", "entry_id = ? AND entry_type = ?", "status IN ('running', 'waiting')", "RETURNING id, workspace_id, workflow_id, current_node_id"} {
		if !strings.Contains(cancelActiveAgentRunsSQL, fragment) {
			t.Fatalf("statement misses %q:\n%s", fragment, cancelActiveAgentRunsSQL)
		}
	}

	db, mock := mockDB(t)
	mock.ExpectQuery(sqlPattern(cancelActiveAgentRunsSQL)).WithArgs(workflow.CancelReasonConversationFinished, "e-1", "whatsapp").WillReturnRows(
		sqlmock.NewRows([]string{"id", "workspace_id", "workflow_id", "current_node_id"}).AddRow("r-1", "ws-1", "wf-1", "n7"),
	)

	got, err := NewAgentCallRunsRepository(db).CancelActiveAgentRuns("e-1", "whatsapp", workflow.CancelReasonConversationFinished)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != (workflow.CancelledAgentRun{RunID: "r-1", WorkspaceID: "ws-1", WorkflowID: "wf-1", CurrentNodeID: "n7"}) {
		t.Fatalf("cancelled = %+v", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestAContinuationIsClaimedOncePerRunBeforeAnythingIsSent(t *testing.T) {
	if got := strings.Count(claimAgentContinuationSQL, "?"); got != 1 {
		t.Fatalf("placeholders = %d, want 1", got)
	}
	for _, fragment := range []string{"SET agent_continued_at = now()", "WHERE id = ?", "agent_continued_at IS NULL", "started_by LIKE 'agent:%'"} {
		if !strings.Contains(claimAgentContinuationSQL, fragment) {
			t.Fatalf("statement misses %q:\n%s", fragment, claimAgentContinuationSQL)
		}
	}

	db, mock := mockDB(t)
	mock.ExpectExec(sqlPattern(claimAgentContinuationSQL)).WithArgs("r-1").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(sqlPattern(claimAgentContinuationSQL)).WithArgs("r-1").WillReturnResult(sqlmock.NewResult(0, 0))

	repo := NewAgentCallRunsRepository(db)
	first, err := repo.ClaimAgentContinuation("r-1")
	if err != nil || !first {
		t.Fatalf("first claim = %v, %v; want true", first, err)
	}
	second, err := repo.ClaimAgentContinuation("r-1")
	if err != nil || second {
		t.Fatalf("second claim = %v, %v; a retry or a second worker must not continue again", second, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestTheContinuationBacklogReadsOnlyUnclaimedAgentRunsThatFinishedInTheWindow(t *testing.T) {
	for _, fragment := range []string{
		"started_by LIKE 'agent:%'",
		"agent_continued_at IS NULL",
		"status IN ('completed', 'error')",
		"updated_at >= ? AND updated_at < ?",
		"LIMIT ?",
	} {
		if !strings.Contains(agentRunsAwaitingContinuationSQL, fragment) {
			t.Fatalf("backlog query misses %q:\n%s", fragment, agentRunsAwaitingContinuationSQL)
		}
	}
	db, mock := mockDB(t)
	from := time.Date(2026, 10, 19, 11, 45, 0, 0, time.UTC)
	before := from.Add(14 * time.Minute)
	mock.ExpectQuery(sqlPattern(agentRunsAwaitingContinuationSQL)).WithArgs(from, before, 500).
		WillReturnRows(sqlmock.NewRows([]string{"id", "workflow_id", "workspace_id", "entry_id", "entry_type", "status", "started_by", "agent_result", "updated_at"}).
			AddRow("run-1", "wf-1", "ws-1", "e-1", "instagram", "completed", "agent:ag-1", `{"status":"ok"}`, before))

	runs, err := NewAgentCallRunsRepository(db).FinishedWithoutContinuation(from, before, 500)
	if err != nil || len(runs) != 1 {
		t.Fatalf("runs = %+v, err = %v", runs, err)
	}
	ev, ok := workflow.AgentRunFinishedOf(runs[0])
	if !ok || ev.Continuation() != workflow.ContinuationResult || ev.AgentID != "ag-1" || !ev.FinishedAt.Equal(before) {
		t.Fatalf("event = %+v, ok = %v", ev, ok)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
