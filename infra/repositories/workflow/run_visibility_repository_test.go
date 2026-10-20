package workflow_repository

import (
	"database/sql/driver"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"vozko/domain/shared"
	"vozko/domain/workflow"
)

func TestAViewerWhoSeesEveryConversationListsRunsWithoutTheVisibilityPredicate(t *testing.T) {
	db, mock := mockDB(t)
	mock.ExpectQuery(sqlPattern(`SELECT count(*) FROM "workflow_runs" WHERE workspace_id = ? AND workflow_id = ?`)).
		WithArgs("ws-1", "wf-1").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectQuery(sqlPattern(`SELECT * FROM "workflow_runs" WHERE workspace_id = ? AND workflow_id = ? ORDER BY started_at DESC LIMIT ?`)).
		WithArgs("ws-1", "wf-1", 20).WillReturnRows(sqlmock.NewRows([]string{"id"}))

	if _, err := NewWorkflowRunRepository(db).List(workflow.ListRunsInput{WorkspaceID: "ws-1", WorkflowID: "wf-1"}); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestTheConversationRunListIsOneIndexedQueryPerPage(t *testing.T) {
	if got := strings.Count(conversationRunsSQL, "?"); got != 5 {
		t.Fatalf("placeholders = %d", got)
	}
	for _, fragment := range []string{
		"WHERE r.workspace_id = ? AND r.entry_id = ? AND r.entry_type = ?",
		"ORDER BY r.started_at DESC",
		"CASE WHEN r.status IN ('running', 'waiting') THEN " + runCurrentNodeTypeSQL + " END AS current_node_type",
		runPinnedGraphJoinSQL,
	} {
		if !strings.Contains(conversationRunsSQL, fragment) {
			t.Fatalf("statement misses %q:\n%s", fragment, conversationRunsSQL)
		}
	}
	if !strings.Contains(latestAgentCallRunSQL, runCurrentNodeTypeSQL) || !strings.Contains(latestAgentCallRunSQL, runPinnedGraphJoinSQL) {
		t.Fatal("the agent run read model must read the current step the same way")
	}
}

func TestTheConversationRunsAreReadNewestFirstWithTheirWorkflow(t *testing.T) {
	db, mock := mockDB(t)
	started := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	completed := started.Add(2 * time.Second)
	mock.ExpectQuery(sqlPattern(countConversationRunsSQL)).WithArgs("ws-1", "e-1", "whatsapp").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(21))
	mock.ExpectQuery(sqlPattern(conversationRunsSQL)).WithArgs("ws-1", "e-1", "whatsapp", 20, 20).
		WillReturnRows(sqlmock.NewRows([]string{"id", "workflow_id", "workflow_name", "workflow_version", "entry_id", "entry_type", "status", "trigger_type", "started_by", "started_at", "completed_at", "wait_reason", "current_node_id", "current_node_type"}).
			AddRow("r-2", "wf-1", "Matrícula", 3, "e-1", "whatsapp", "waiting", "trigger_agent_call", "agent:ag-1", started, nil, "reply", "n4", "action_wait_reply").
			AddRow("r-1", "wf-2", "Boas-vindas", nil, "e-1", "whatsapp", "completed", "trigger_message", "trigger:trigger_message", started, completed, "", "n9", nil))

	page, err := NewConversationRunsRepository(db).ListByEntry("ws-1", "e-1", "whatsapp", shared.Pagination{Page: 2, PageSize: 20})
	if err != nil {
		t.Fatal(err)
	}
	if page.TotalItems != 21 || len(page.Items) != 2 {
		t.Fatalf("page = %+v", page)
	}
	live, ended := page.Items[0], page.Items[1]
	if live.WorkflowName != "Matrícula" || live.WorkflowVersion == nil || *live.WorkflowVersion != 3 || live.CurrentNodeType != "action_wait_reply" || live.WaitReason != workflow.WaitReasonReply || live.StartedBy != "agent:ag-1" {
		t.Fatalf("live = %+v", live)
	}
	if ended.WorkflowVersion != nil || ended.CompletedAt == nil || ended.TriggerType != workflow.TriggerType("trigger_message") {
		t.Fatalf("ended = %+v", ended)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func argMatchers(values []interface{}) []driver.Value {
	out := make([]driver.Value, len(values))
	for i, v := range values {
		out[i] = v
	}
	return out
}
