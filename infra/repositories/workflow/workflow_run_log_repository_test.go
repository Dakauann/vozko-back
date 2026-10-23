package workflow_repository

import (
	"database/sql/driver"
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"vozko/domain/workflow"
)

func mockDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB, PreferSimpleProtocol: true}), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	return db, mock
}

type jsonArg struct {
	t    *testing.T
	want string
}

func (a jsonArg) Match(v driver.Value) bool {
	got, ok := v.(string)
	if !ok {
		a.t.Errorf("want a JSON string argument, got %T %v", v, v)
		return false
	}
	var gotValue, wantValue interface{}
	_ = json.Unmarshal([]byte(got), &gotValue)
	_ = json.Unmarshal([]byte(a.want), &wantValue)
	g, _ := json.Marshal(gotValue)
	w, _ := json.Marshal(wantValue)
	if string(g) != string(w) {
		a.t.Errorf("json argument = %s, want %s", got, a.want)
		return false
	}
	return true
}

func TestPickedStepQueriesBindEveryPlaceholder(t *testing.T) {
	for sql, want := range map[string]int{latestPinnableStepsSQL: 3, stepOfRunSQL: 2, firstTriggerStepSQL: 2} {
		if got := strings.Count(sql, "?"); got != want {
			t.Fatalf("%s\nplaceholders = %d, want %d", sql, got, want)
		}
	}
}

type nullArg struct{}

func (nullArg) Match(v driver.Value) bool { return v == nil }

var stepColumns = []string{
	"id", "run_id", "node_id", "node_type", "status", "input", "output", "error", "executed_at",
	"attempt", "started_at", "finished_at", "duration_ms", "branch", "error_kind", "error_chain", "stack", "script_logs", "http", "unresolved_refs",
}

func TestAStepIsStoredWithOneInsertCarryingTheWholeTrace(t *testing.T) {
	db, mock := mockDB(t)
	started := time.Date(2026, 10, 19, 20, 15, 27, 0, time.UTC)
	finished := started.Add(1200 * time.Millisecond)
	entry := &workflow.WorkflowRunLog{
		ID: "11111111-1111-1111-1111-111111111111", RunID: "22222222-2222-2222-2222-222222222222",
		NodeID: "n55", NodeType: workflow.NodeTypeActionHTTPRequest, Status: workflow.LogStatusFailed,
		Input: map[string]interface{}{"url": "https://api"}, Error: "HTTP 500",
		Attempt: 2, StartedAt: &started, FinishedAt: &finished, DurationMs: 1200, Branch: "falha",
		ErrorKind: workflow.ErrorKindHTTPStatus, ErrorChain: []string{"HTTP 500"},
		ScriptLogs:     []workflow.ScriptLog{{Level: "info", Message: "etapa"}},
		HTTP:           []workflow.HTTPExchange{{Method: "POST", URL: "https://api", Status: 500, LatencyMs: 30}},
		UnresolvedRefs: []string{"input.cpf"},
	}

	insert := `^INSERT INTO "workflow_run_logs" \(` + strings.Join(quoted(stepColumns), ",") + `\) VALUES \(` + placeholders(len(stepColumns)) + `\)$`
	mock.ExpectBegin()
	mock.ExpectExec(insert).WithArgs(
		entry.ID, entry.RunID, "n55", "action_http_request", "failed",
		jsonArg{t, `{"url":"https://api"}`}, nullArg{}, "HTTP 500", sqlmock.AnyArg(),
		2, started, finished, int64(1200), "falha", "http_status",
		jsonArg{t, `["HTTP 500"]`}, nullArg{}, jsonArg{t, `[{"level":"info","message":"etapa"}]`},
		jsonArg{t, `[{"method":"POST","url":"https://api","status":500,"latencyMs":30}]`}, jsonArg{t, `["input.cpf"]`},
	).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	if err := NewWorkflowRunLogRepository(db).Create(entry); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestAPageOfStepsIsReadInRunOrderWithItsBounds(t *testing.T) {
	db, mock := mockDB(t)
	started := time.Date(2026, 10, 19, 20, 15, 27, 0, time.UTC)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "workflow_run_logs" WHERE run_id = $1 ORDER BY executed_at ASC, id ASC LIMIT $2 OFFSET $3`)).
		WithArgs("run-1", 50, 100).
		WillReturnRows(sqlmock.NewRows(stepColumns).AddRow(
			"l1", "run-1", "n56", "action_code", "executed", nil, `{"a":1}`, "", started,
			1, started, started, int64(1), "", "", nil, nil, nil, nil, nil,
		))

	logs, err := NewWorkflowRunLogRepository(db).PageByRunID("run-1", 100, 50)
	if err != nil || len(logs) != 1 || logs[0].NodeID != "n56" || logs[0].Output["a"] != 1.0 {
		t.Fatalf("logs = %+v err = %v", logs, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestAStepReadsBackItsTraceAndPrunedPayloadsAsMissing(t *testing.T) {
	db, mock := mockDB(t)
	started := time.Date(2026, 10, 19, 20, 15, 27, 0, time.UTC)
	rows := sqlmock.NewRows(stepColumns).AddRow(
		"l1", "run-1", "n56", "action_code", "failed", nil, nil, "boom", started,
		3, started, started.Add(time.Second), int64(1000), "", "script", `["action_code: boom","boom"]`, "Error: boom\n\tat (3:5)",
		`[{"level":"warn","message":"x"}]`, nil, `["lead_name"]`,
	).AddRow(
		"l0", "run-1", "n1", "trigger_webhook", "executed", `null`, `{}`, "", started,
		nil, nil, nil, nil, "", "", nil, nil, nil, nil, nil,
	)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "workflow_run_logs" WHERE run_id = $1 ORDER BY executed_at ASC`)).
		WithArgs("run-1").WillReturnRows(rows)

	logs, err := NewWorkflowRunLogRepository(db).FindByRunID("run-1")
	if err != nil {
		t.Fatal(err)
	}
	step := logs[0]
	if step.Input != nil || step.Output != nil || step.Attempt != 3 || step.DurationMs != 1000 || step.ErrorKind != workflow.ErrorKindScript {
		t.Fatalf("step = %+v", step)
	}
	if len(step.ErrorChain) != 2 || step.Stack == "" || len(step.ScriptLogs) != 1 || step.StartedAt == nil || step.FinishedAt == nil || len(step.UnresolvedRefs) != 1 {
		t.Fatalf("trace = %+v", step)
	}
	legacy := logs[1]
	if legacy.Attempt != 0 || legacy.StartedAt != nil || legacy.Input != nil || legacy.Output == nil {
		t.Fatalf("legacy row = %+v", legacy)
	}
}

func quoted(columns []string) []string {
	out := make([]string, len(columns))
	for i, c := range columns {
		out[i] = `"` + c + `"`
	}
	return out
}

func placeholders(n int) string {
	parts := make([]string, n)
	for i := range parts {
		parts[i] = `\$` + strconv.Itoa(i+1)
	}
	return strings.Join(parts, ",")
}
