package ai_attendance_repository

import (
	"database/sql/driver"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	aa "vozko/domain/ai_attendance"
	"vozko/domain/shared"
)

func pattern(query string) string {
	return "^" + strings.ReplaceAll(regexp.QuoteMeta(query), `\?`, `\$[0-9]+`) + "$"
}

type containsArg string

func (c containsArg) Match(v driver.Value) bool {
	s, ok := v.(string)
	return ok && strings.Contains(s, string(c))
}

func TestATurnsToolCallsAreWrittenInOneInsert(t *testing.T) {
	db, mock := mockDB(t)
	at := time.Date(2026, 10, 19, 20, 0, 0, 0, time.UTC)
	calls := []aa.ToolCall{
		{ID: "c-1", ToolCallID: "call_model_1", WorkspaceID: "ws-1", EntryID: "e-1", EntryType: "whatsapp", AgentID: "a-1", SessionID: "s-1", ToolName: "http_request", Args: map[string]interface{}{"url": "https://api"}, Outcome: aa.ToolCallOK, DurationMs: 120, CreatedAt: at},
		{ID: "c-2", WorkspaceID: "ws-1", EntryID: "e-1", EntryType: "whatsapp", AgentID: "a-1", ToolName: "transfer_to_human", Outcome: aa.ToolCallError, Error: "409", ErrorCode: "workflow_not_active", CreatedAt: at},
	}
	mock.ExpectExec(`^INSERT INTO "ai_tool_calls" \("id","tool_call_id","workspace_id","entry_id","entry_type","agent_id","session_id","tool_name","args","outcome","error","error_code","duration_ms","run_id","created_at"\) VALUES \(.+\),\(.+\)$`).
		WithArgs(
			"c-1", "call_model_1", "ws-1", "e-1", "whatsapp", "a-1", "s-1", "http_request", containsArg(`"url":"https://api"`), "ok", "", "", int64(120), nil, at,
			"c-2", nil, "ws-1", "e-1", "whatsapp", "a-1", "", "transfer_to_human", nil, "error", "409", "workflow_not_active", int64(0), nil, at,
		).
		WillReturnResult(sqlmock.NewResult(0, 2))

	if err := NewToolCallLog(db).Record(calls); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	if err := NewToolCallLog(db).Record(nil); err != nil {
		t.Fatalf("an empty batch writes nothing: %v", err)
	}
}

func TestAConversationsToolCallsAreListedNewestFirstInsideTheWorkspace(t *testing.T) {
	db, mock := mockDB(t)
	at := time.Date(2026, 10, 19, 20, 0, 0, 0, time.UTC)
	mock.ExpectQuery(pattern(`SELECT count(*) FROM "ai_tool_calls" WHERE workspace_id = ? AND entry_id = ? AND entry_type = ?`)).
		WithArgs("ws-1", "e-1", "whatsapp").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	mock.ExpectQuery(pattern(`SELECT ai_tool_calls.*, wr.workflow_id AS run_workflow_id FROM "ai_tool_calls" `+toolCallRunJoinSQL+` WHERE ai_tool_calls.workspace_id = ? AND ai_tool_calls.entry_id = ? AND ai_tool_calls.entry_type = ? ORDER BY ai_tool_calls.created_at DESC LIMIT ?`)).
		WithArgs("ws-1", "e-1", "whatsapp", 20).
		WillReturnRows(sqlmock.NewRows([]string{"id", "tool_call_id", "workspace_id", "entry_id", "entry_type", "agent_id", "session_id", "tool_name", "args", "outcome", "error", "duration_ms", "run_id", "created_at", "run_workflow_id"}).
			AddRow("c-1", "call_model_1", "ws-1", "e-1", "whatsapp", "a-1", "s-1", "http_request", `{"url":"https://api"}`, "ok", "", 120, "r-1", at, "wf-1"))

	page, err := NewToolCallLog(db).ListByEntry("ws-1", "e-1", "whatsapp", shared.Pagination{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatal(err)
	}
	if page.TotalItems != 1 || len(page.Items) != 1 {
		t.Fatalf("page = %+v", page)
	}
	got := page.Items[0]
	if got.ToolCallID != "call_model_1" || got.ToolName != "http_request" || got.Args["url"] != "https://api" || got.RunID != "r-1" || got.WorkflowID != "wf-1" || got.Outcome != aa.ToolCallOK || got.DurationMs != 120 {
		t.Fatalf("call = %+v", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestOldToolCallsAreDeletedInBoundedBatches(t *testing.T) {
	db, mock := mockDB(t)
	cutoff := time.Date(2026, 4, 22, 0, 0, 0, 0, time.UTC)
	if got := strings.Count(deleteToolCallsBeforeSQL, "?"); got != 2 {
		t.Fatalf("placeholders = %d, want 2", got)
	}
	mock.ExpectExec(pattern(deleteToolCallsBeforeSQL)).WithArgs(cutoff, 5000).WillReturnResult(sqlmock.NewResult(0, 12))

	deleted, err := NewToolCallLog(db).DeleteCreatedBefore(cutoff, 5000)
	if err != nil || deleted != 12 {
		t.Fatalf("deleted = %d, err = %v", deleted, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
