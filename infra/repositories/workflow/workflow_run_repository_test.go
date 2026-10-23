package workflow_repository

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"vozko/domain/workflow"
)

func TestARunIsCreatedWithWhoStartedItAndHow(t *testing.T) {
	db, mock := mockDB(t)
	version := 4
	run := &workflow.WorkflowRun{
		ID: "33333333-3333-3333-3333-333333333333", WorkflowID: "wf-1", WorkspaceID: "ws-1", EntryID: "e-1", EntryType: "whatsapp",
		Status: workflow.RunStatusRunning, TriggerNodeID: "n1", CurrentNodeID: "n1", State: workflow.NewRunState(),
		TriggerType: workflow.TriggerWebhook, StartedBy: workflow.StartedByWebhook("d-1"), WorkflowVersion: &version,
	}

	mock.ExpectBegin()
	mock.ExpectExec(`^INSERT INTO "workflow_runs" \(.*"trigger_type","started_by","workflow_version","agent_result","trigger_data"\) VALUES`).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	if err := NewWorkflowRunRepository(db).Create(run); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

type jsonContaining string

func (want jsonContaining) Match(v driver.Value) bool {
	s, ok := v.(*string)
	if ok && s != nil {
		return strings.Contains(*s, string(want))
	}
	text, ok := v.(string)
	return ok && strings.Contains(text, string(want))
}

func TestARunIsCreatedWithItsTriggerDataAndAStartWithoutDataStoresNull(t *testing.T) {
	db, mock := mockDB(t)
	run := &workflow.WorkflowRun{
		ID: "33333333-3333-3333-3333-333333333333", WorkflowID: "wf-1", WorkspaceID: "ws-1", EntryID: "e-1", EntryType: "whatsapp",
		Status: workflow.RunStatusRunning, State: workflow.NewRunState(), TriggerData: map[string]interface{}{"curso": "ADS"},
	}
	empty := *run
	empty.ID = "44444444-4444-4444-4444-444444444444"
	empty.TriggerData = nil

	mock.ExpectBegin()
	mock.ExpectExec(`^INSERT INTO "workflow_runs"`).
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
			sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
			sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), jsonContaining(`"curso":"ADS"`)).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()
	mock.ExpectBegin()
	mock.ExpectExec(`^INSERT INTO "workflow_runs"`).
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
			sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
			sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), nil).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	repo := NewWorkflowRunRepository(db)
	if err := repo.Create(run); err != nil {
		t.Fatal(err)
	}
	if err := repo.Create(&empty); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestARunReadsBackItsTriggerData(t *testing.T) {
	db, mock := mockDB(t)
	now := time.Now().UTC()
	rows := sqlmock.NewRows([]string{"id", "workflow_id", "workspace_id", "entry_id", "entry_type", "status", "state", "started_at", "updated_at", "trigger_data"}).
		AddRow("r-1", "wf-1", "ws-1", "e-1", "whatsapp", "completed", "{}", now, now, `{"curso":"ADS"}`)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "workflow_runs" WHERE id = $1`)).WillReturnRows(rows)

	run, err := NewWorkflowRunRepository(db).FindByID("r-1")
	if err != nil {
		t.Fatal(err)
	}
	if run.TriggerData["curso"] != "ADS" {
		t.Fatalf("trigger data = %v", run.TriggerData)
	}
}

func TestARunUpdateKeepsTheAgentResult(t *testing.T) {
	db, mock := mockDB(t)
	run := &workflow.WorkflowRun{ID: "r-1", Status: workflow.RunStatusCompleted, State: workflow.NewRunState(), AgentResult: map[string]interface{}{"status": "ok"}}

	mock.ExpectBegin()
	mock.ExpectExec(`^UPDATE "workflow_runs" SET .*"agent_result"=\$\d+.* WHERE id = \$\d+ AND status NOT IN \(\$\d+,\$\d+,\$\d+\)$`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	if err := NewWorkflowRunRepository(db).Update(run); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestAFinishedRunIsNeverRewrittenByALateSave(t *testing.T) {
	db, mock := mockDB(t)
	run := &workflow.WorkflowRun{ID: "r-1", Status: workflow.RunStatusRunning, State: workflow.NewRunState()}

	mock.ExpectBegin()
	mock.ExpectExec(`^UPDATE "workflow_runs" SET .* WHERE id = \$\d+ AND status NOT IN \(\$\d+,\$\d+,\$\d+\)$`).
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
			sqlmock.AnyArg(), "r-1", "completed", "error", "cancelled").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()

	err := NewWorkflowRunRepository(db).Update(run)
	if !errors.Is(err, workflow.ErrRunTerminal) {
		t.Fatalf("err = %v, want ErrRunTerminal so the caller stops instead of reviving a cancelled run", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestAnAgentResultThatCannotBeEncodedFailsTheWriteInsteadOfDroppingTheContinuation(t *testing.T) {
	db, mock := mockDB(t)
	unencodable := map[string]interface{}{"callback": func() {}}
	repo := NewWorkflowRunRepository(db)

	for name, write := range map[string]func() error{
		"update": func() error {
			return repo.Update(&workflow.WorkflowRun{ID: "r-1", Status: workflow.RunStatusCompleted, State: workflow.NewRunState(), AgentResult: unencodable})
		},
		"create": func() error {
			return repo.Create(&workflow.WorkflowRun{ID: "r-2", Status: workflow.RunStatusRunning, State: workflow.NewRunState(), AgentResult: unencodable})
		},
	} {
		var unsupported *json.UnsupportedTypeError
		if err := write(); !errors.As(err, &unsupported) {
			t.Fatalf("%s: err = %v, want the encoding error so agent_result is never written as NULL", name, err)
		}
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestAnOperatorCancelWritesOnlyTheStatusOfALiveRun(t *testing.T) {
	if got := strings.Count(cancelRunSQL, "?"); got != 2 {
		t.Fatalf("placeholders = %d, want the reason and the id", got)
	}
	for name, affected := range map[string]int64{"live": 1, "already finished": 0} {
		t.Run(name, func(t *testing.T) {
			db, mock := mockDB(t)
			mock.ExpectExec(`^UPDATE workflow_runs SET status = 'cancelled', error = \$1, wake_at = NULL, completed_at = now\(\), updated_at = now\(\)\s+WHERE id = \$2 AND status IN \('running', 'waiting'\)$`).
				WithArgs(workflow.CancelReasonOperator, "r-1").
				WillReturnResult(sqlmock.NewResult(0, affected))

			cancelled, err := NewWorkflowRunRepository(db).(workflow.RunCanceller).CancelRun("r-1", workflow.CancelReasonOperator)
			if err != nil || cancelled != (affected == 1) {
				t.Fatalf("cancelled = %v err = %v", cancelled, err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestTheEngineReadsOnlyTheStatusOfARunAtEachStep(t *testing.T) {
	db, mock := mockDB(t)
	mock.ExpectQuery(`^SELECT "status" FROM "workflow_runs" WHERE id = \$1 LIMIT \$2$`).
		WithArgs("r-1", 1).
		WillReturnRows(sqlmock.NewRows([]string{"status"}).AddRow("cancelled"))

	status, err := NewWorkflowRunRepository(db).(workflow.RunStatusReader).RunStatus("r-1")
	if err != nil || status != workflow.RunStatusCancelled {
		t.Fatalf("status = %q, err = %v", status, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestAGoneRunHasNoStatusToContinueFrom(t *testing.T) {
	db, mock := mockDB(t)
	mock.ExpectQuery(`^SELECT "status" FROM "workflow_runs" WHERE id = \$1 LIMIT \$2$`).
		WillReturnRows(sqlmock.NewRows([]string{"status"}))

	_, err := NewWorkflowRunRepository(db).(workflow.RunStatusReader).RunStatus("r-1")
	if !errors.Is(err, workflow.ErrRunNotFound) {
		t.Fatalf("err = %v, want ErrRunNotFound", err)
	}
}

func TestARunReadsBackItsStarterVersionAndAgentResult(t *testing.T) {
	db, mock := mockDB(t)
	now := time.Now().UTC()
	rows := sqlmock.NewRows([]string{"id", "workflow_id", "workspace_id", "entry_id", "entry_type", "status", "state", "started_at", "updated_at", "trigger_type", "started_by", "workflow_version", "agent_result"}).
		AddRow("r-1", "wf-1", "ws-1", "e-1", "whatsapp", "completed", "{}", now, now, "trigger_webhook", "webhook:d-1", 4, `{"status":"ok"}`)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "workflow_runs" WHERE id = $1`)).WillReturnRows(rows)

	run, err := NewWorkflowRunRepository(db).FindByID("r-1")
	if err != nil {
		t.Fatal(err)
	}
	if run.TriggerType != workflow.TriggerWebhook || run.StartedBy != "webhook:d-1" || run.WorkflowVersion == nil || *run.WorkflowVersion != 4 || run.AgentResult["status"] != "ok" {
		t.Fatalf("run = %+v", run)
	}
}
