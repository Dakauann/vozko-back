package ai_attendance_repository

import (
	"testing"
	"time"

	"github.com/google/uuid"

	aa "vozko/domain/ai_attendance"
	"vozko/domain/shared"
	"vozko/infra/database/schema"
	"vozko/infra/repositories/repotest"
)

func TestAToolCallReadsTheWorkflowOfItsRunOnlyInsideItsOwnWorkspaceOnPostgres(t *testing.T) {
	db := repotest.IsolatedDB(t, "aitc_wf", &schema.AIToolCallSchema{}, &schema.WorkflowRunSchema{})
	workspaceID, otherWorkspace := uuid.NewString(), uuid.NewString()
	entryID := uuid.NewString()
	ownRun, foreignRun := uuid.NewString(), uuid.NewString()
	workflowID, foreignWorkflow := uuid.NewString(), uuid.NewString()
	at := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)

	for _, run := range []schema.WorkflowRunSchema{
		{ID: ownRun, WorkflowID: workflowID, WorkspaceID: workspaceID, EntryID: entryID, EntryType: "whatsapp", Status: "completed", State: "{}", StartedAt: at},
		{ID: foreignRun, WorkflowID: foreignWorkflow, WorkspaceID: otherWorkspace, EntryID: entryID, EntryType: "whatsapp", Status: "completed", State: "{}", StartedAt: at},
	} {
		if err := db.Create(&run).Error; err != nil {
			t.Fatalf("seed run: %v", err)
		}
	}
	calls := []aa.ToolCall{
		{ID: uuid.NewString(), ToolCallID: "toolu_01AbC", WorkspaceID: workspaceID, EntryID: entryID, EntryType: "whatsapp", AgentID: "a-1", ToolName: "run_workflow_x", Outcome: aa.ToolCallOK, RunID: ownRun, CreatedAt: at.Add(3 * time.Second)},
		{ID: uuid.NewString(), WorkspaceID: workspaceID, EntryID: entryID, EntryType: "whatsapp", AgentID: "a-1", ToolName: "run_workflow_y", Outcome: aa.ToolCallOK, RunID: foreignRun, CreatedAt: at.Add(2 * time.Second)},
		{ID: uuid.NewString(), WorkspaceID: workspaceID, EntryID: entryID, EntryType: "whatsapp", AgentID: "a-1", ToolName: "http_request", Outcome: aa.ToolCallOK, CreatedAt: at.Add(time.Second)},
	}
	log := NewToolCallLog(db)
	if err := log.Record(calls); err != nil {
		t.Fatalf("record: %v", err)
	}

	page, err := log.ListByEntry(workspaceID, entryID, "whatsapp", shared.Pagination{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if page.TotalItems != 3 || len(page.Items) != 3 {
		t.Fatalf("page = %+v", page)
	}
	if got := page.Items[0]; got.RunID != ownRun || got.WorkflowID != workflowID || got.ToolCallID != "toolu_01AbC" {
		t.Fatalf("the call that started a run in this workspace names its workflow and keeps its tool call id: %+v", got)
	}
	if got := page.Items[2]; got.ToolCallID != "" {
		t.Fatalf("an older call without a tool call id reads as none: %+v", got)
	}
	if got := page.Items[1]; got.RunID != foreignRun || got.WorkflowID != "" {
		t.Fatalf("a run of another workspace must never lend its workflow id: %+v", got)
	}
	if got := page.Items[2]; got.RunID != "" || got.WorkflowID != "" {
		t.Fatalf("a call without a run has no workflow: %+v", got)
	}
}
