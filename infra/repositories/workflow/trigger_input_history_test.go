package workflow_repository

import (
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"

	"vozko/domain/conversation"
	"vozko/domain/workflow"
	"vozko/infra/database"
)

func TestTheLatestTriggerDataIsOneIndexedQueryForAViewerWhoSeesEverything(t *testing.T) {
	if got := strings.Count(latestTriggerDataSQL(""), "?"); got != 4 {
		t.Fatalf("placeholders = %d", got)
	}
	db, mock := mockDB(t)
	started := time.Date(2026, 10, 21, 15, 0, 0, 0, time.UTC)
	mock.ExpectQuery(sqlPattern(latestTriggerDataSQL(""))).WithArgs("ws-1", "wf-1", "trigger_agent_call", workflow.TriggerInputHistoryWindow).
		WillReturnRows(sqlmock.NewRows([]string{"id", "started_at", "trigger_data"}).AddRow("run-1", started, `{"input":{"cpf":"1"}}`))

	got, err := NewTriggerInputHistory(db).LatestTriggerData("ws-1", "wf-1", workflow.TriggerAgentCall, conversation.VisibilityScope{})
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.RunID != "run-1" || !got.StartedAt.Equal(started) || got.Data["input"].(map[string]interface{})["cpf"] != "1" {
		t.Fatalf("got = %+v", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestNoRunWithTriggerDataReadsAsNothing(t *testing.T) {
	db, mock := mockDB(t)
	mock.ExpectQuery(sqlPattern(latestTriggerDataSQL(""))).WithArgs("ws-1", "wf-1", "trigger_webhook", workflow.TriggerInputHistoryWindow).
		WillReturnRows(sqlmock.NewRows([]string{"id", "started_at", "trigger_data"}))
	got, err := NewTriggerInputHistory(db).LatestTriggerData("ws-1", "wf-1", workflow.TriggerWebhook, conversation.VisibilityScope{})
	if err != nil || got != nil {
		t.Fatalf("got = %+v, err = %v", got, err)
	}
}

func (f *sqlFixture) triggeredRun(workflowID, entryType, triggerType string, startedAt time.Time, triggerData *string) string {
	f.t.Helper()
	id := uuid.NewString()
	f.exec(`INSERT INTO workflow_runs (id, workflow_id, workspace_id, entry_id, entry_type, status, current_node_id, state, started_at, updated_at, trigger_type, trigger_data)
		VALUES (?, ?, ?, ?, ?, 'completed', 't', '{}'::jsonb, ?, ?, ?, ?::jsonb)`,
		id, workflowID, f.workspace, uuid.NewString(), entryType, startedAt, startedAt, triggerType, triggerData)
	return id
}

func TestTheLatestTriggerDataAgainstPostgresSkipsOtherTriggersEmptyDataAndHiddenConversations(t *testing.T) {
	f := newSQLFixture(t, visibilityTables()...)
	wf, other := f.workflow(nil, `{"nodes":[],"edges":[]}`), f.workflow(nil, `{"nodes":[],"edges":[]}`)
	data := func(cpf string) *string { s := `{"input":{"cpf":"` + cpf + `"}}`; return &s }
	agent, webhook := string(workflow.TriggerAgentCall), string(workflow.TriggerWebhook)

	lead := f.triggeredRun(wf, workflow.EntryTypeLead, agent, f.at.Add(-3*time.Hour), data("lead"))
	conversationRun := f.triggeredRun(wf, "whatsapp", agent, f.at.Add(-2*time.Hour), data("conversa"))
	f.triggeredRun(wf, "whatsapp", agent, f.at.Add(-time.Hour), nil)
	f.triggeredRun(wf, "whatsapp", webhook, f.at, data("webhook"))
	f.triggeredRun(other, "whatsapp", agent, f.at, data("outro"))

	history := NewTriggerInputHistory(f.db)
	got, err := history.LatestTriggerData(f.workspace, wf, workflow.TriggerAgentCall, conversation.VisibilityScope{})
	if err != nil || got == nil || got.RunID != conversationRun {
		t.Fatalf("got = %+v err = %v, want the newest run of this trigger that kept its data", got, err)
	}
	hidden, err := history.LatestTriggerData(f.workspace, wf, workflow.TriggerAgentCall, conversation.NothingVisible())
	if err != nil || hidden == nil || hidden.RunID != lead {
		t.Fatalf("got = %+v err = %v, a hidden conversation run must count as none", hidden, err)
	}
	none, err := history.LatestTriggerData(uuid.NewString(), wf, workflow.TriggerAgentCall, conversation.VisibilityScope{})
	if err != nil || none != nil {
		t.Fatalf("another workspace must read nothing, got %+v err = %v", none, err)
	}

	ddl, ok := database.ConcurrentIndexSQL(database.WorkflowRunTriggerInputIndex)
	if !ok {
		t.Fatalf("%s is not declared", database.WorkflowRunTriggerInputIndex)
	}
	f.exec(strings.Replace(ddl, "CONCURRENTLY ", "", 1))
	f.exec("SET LOCAL enable_seqscan = off")
	var plan []string
	if err := f.db.Raw("EXPLAIN "+latestTriggerDataSQL(""), f.workspace, wf, agent, workflow.TriggerInputHistoryWindow).Scan(&plan).Error; err != nil {
		t.Fatal(err)
	}
	if joined := strings.Join(plan, "\n"); !strings.Contains(joined, database.WorkflowRunTriggerInputIndex) {
		t.Fatalf("the read must use %s:\n%s", database.WorkflowRunTriggerInputIndex, joined)
	}
}

func TestARestrictedViewerReadsOnlyTheNewestWindowOfRunsSoHiddenRunsCannotMakeTheReadUnbounded(t *testing.T) {
	f := newSQLFixture(t, visibilityTables()...)
	wf := f.workflow(nil, `{"nodes":[],"edges":[]}`)
	data := `{"input":{"cpf":"1"}}`
	agent := string(workflow.TriggerAgentCall)
	f.triggeredRun(wf, workflow.EntryTypeLead, agent, f.at.Add(-time.Duration(workflow.TriggerInputHistoryWindow+1)*time.Minute), &data)
	for i := 0; i < workflow.TriggerInputHistoryWindow; i++ {
		f.triggeredRun(wf, "whatsapp", agent, f.at.Add(-time.Duration(i)*time.Minute), &data)
	}

	history := NewTriggerInputHistory(f.db)
	hidden, err := history.LatestTriggerData(f.workspace, wf, workflow.TriggerAgentCall, conversation.NothingVisible())
	if err != nil || hidden != nil {
		t.Fatalf("got = %+v err = %v, a visible run older than the newest %d must read as none", hidden, err, workflow.TriggerInputHistoryWindow)
	}
	everything, err := history.LatestTriggerData(f.workspace, wf, workflow.TriggerAgentCall, conversation.VisibilityScope{})
	if err != nil || everything == nil {
		t.Fatalf("got = %+v err = %v, a viewer who sees everything reads the newest run", everything, err)
	}
}
