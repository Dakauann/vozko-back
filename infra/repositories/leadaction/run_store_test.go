package leadaction_repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"vozko/domain/leadaction"
)

const (
	workspaceID = "8f2d6a4e-1c3b-4d5e-9f7a-0b1c2d3e4f5a"
	runID       = "0b7c3f1e-2d4a-4c5b-9e8f-1a2b3c4d5e6f"
	actorID     = "4b1c8a52-0d9e-4f6a-9c3e-2a7d5b8e1f00"
)

var at = time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)

func mockDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock, *sql.DB) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatal(err)
	}
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB, PreferSimpleProtocol: true, WithoutReturning: true}),
		&gorm.Config{SkipDefaultTransaction: true, Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	return db, mock, sqlDB
}

func columns() []string {
	return []string{"id", "workspace_id", "actor_id", "is_admin", "department_id", "action", "params", "idempotency_key", "request_fingerprint",
		"status", "phase", "cursor", "result", "failure_code", "attempts", "claim_token", "heartbeat_at", "not_before", "started_at", "finished_at", "created_at", "updated_at"}
}

func runRow(rows *sqlmock.Rows, status string, attempts int, claim interface{}) *sqlmock.Rows {
	return rows.AddRow(runID, workspaceID, actorID, false, nil, "block", []byte(`{"blocked":true,"businessPhoneId":"phone-1"}`), "key-1", "fp",
		status, "edit", nil, []byte(`{"matched":3,"selected":3,"processed":0,"changed":0,"skipped":{}}`), nil, attempts, claim, at, nil, at, nil, at, at)
}

func TestStatementsBindEveryPlaceholder(t *testing.T) {
	for name, stmt := range map[string]string{
		"claim": claimSQL, "claimable": claimableSQL, "stalled": failStalledSQL, "save": saveSQL + claimGuardSQL,
	} {
		if strings.Contains(stmt, "@?") || strings.Contains(stmt, "?|") {
			t.Fatalf("%s uses a question mark operator", name)
		}
	}
	if got := strings.Count(claimSQL, "?"); got != 8 {
		t.Fatalf("claim binds %d placeholders", got)
	}
	if got := strings.Count(saveSQL+claimGuardSQL, "?"); got != 14 {
		t.Fatalf("save binds %d placeholders", got)
	}
}

func TestClaimIsACompareAndSetFromQueuedOrStale(t *testing.T) {
	db, mock, _ := mockDB(t)
	s := NewStore(db)

	mock.ExpectQuery(`^UPDATE lead_action_runs SET status = 'running', claim_token = \$1, heartbeat_at = \$2, not_before = NULL, attempts = attempts \+ 1`).
		WithArgs("claim-1", at, at, at, runID, leadaction.MaxAttempts, at, at.Add(-leadaction.StaleAfter)).
		WillReturnRows(runRow(sqlmock.NewRows(columns()), "running", 1, "claim-1"))

	r, err := s.Claim(context.Background(), runID, "claim-1", at)
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != leadaction.StatusRunning || r.Claim != "claim-1" || r.Params.Blocked == nil || !*r.Params.Blocked || r.Result.Matched != 3 {
		t.Fatalf("claimed run = %+v", r)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestClaimLostWhenNoRowMoves(t *testing.T) {
	db, mock, _ := mockDB(t)
	s := NewStore(db)
	mock.ExpectQuery(`^UPDATE lead_action_runs SET status = 'running'`).WillReturnRows(sqlmock.NewRows(columns()))

	if _, err := s.Claim(context.Background(), runID, "claim-2", at); !errors.Is(err, leadaction.ErrClaimLost) {
		t.Fatalf("Claim = %v", err)
	}
	if _, err := s.Claim(context.Background(), "nope", "claim-2", at); !errors.Is(err, leadaction.ErrRunNotFound) {
		t.Fatalf("a malformed id = %v", err)
	}
}

func TestSaveIsGuardedByTheClaim(t *testing.T) {
	db, mock, _ := mockDB(t)
	s := NewStore(db)
	run := &leadaction.Run{ID: runID, WorkspaceID: workspaceID, Status: leadaction.StatusRunning, Phase: leadaction.PhaseEdit, Cursor: runID,
		Result: leadaction.Result{Processed: 500, Skipped: map[leadaction.SkipReason]int{}}, Attempts: 1, Claim: "claim-1", UpdatedAt: at}

	mock.ExpectQuery(regexp.QuoteMeta("UPDATE lead_action_runs SET status = $1, phase = $2, cursor = $3, result = $4::jsonb, failure_code = $5, attempts = $6, claim_token = $7, heartbeat_at = $8, not_before = $9, started_at = $10, finished_at = $11, updated_at = $12 WHERE id = $13 AND claim_token = $14 RETURNING id")).
		WithArgs("running", "edit", runID, sqlmock.AnyArg(), sqlmock.AnyArg(), 1, sqlmock.AnyArg(), nil, nil, nil, nil, at, runID, "claim-1").
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	if err := s.Save(context.Background(), run, "claim-1"); !errors.Is(err, leadaction.ErrClaimLost) {
		t.Fatalf("a save after the claim moved = %v", err)
	}
	if err := s.Save(context.Background(), run, ""); !errors.Is(err, leadaction.ErrClaimLost) {
		t.Fatalf("a save without a claim = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestCreateTellsADuplicateKeyApart(t *testing.T) {
	db, mock, _ := mockDB(t)
	s := NewStore(db)
	run, err := leadaction.NewRun(leadaction.RunRequest{ID: runID, WorkspaceID: workspaceID, ActorID: actorID, Action: leadaction.ActionBlock,
		Params: leadaction.Params{Blocked: new(bool)}, IdempotencyKey: "key-1"}, at)
	if err != nil {
		t.Fatal(err)
	}
	mock.ExpectExec(`^INSERT INTO "lead_action_runs"`).WillReturnError(errors.New(`ERROR: duplicate key value violates unique constraint "ux_lead_action_runs_key" (SQLSTATE 23505)`))

	if err := s.Create(context.Background(), run); !errors.Is(err, leadaction.ErrRunExists) {
		t.Fatalf("Create = %v", err)
	}
}

func TestGetAndFindByKeyReadTheWorkspaceOnly(t *testing.T) {
	db, mock, _ := mockDB(t)
	s := NewStore(db)

	mock.ExpectQuery(regexp.QuoteMeta("SELECT * FROM lead_action_runs WHERE workspace_id = $1 AND id = $2")).
		WithArgs(workspaceID, runID).WillReturnRows(runRow(sqlmock.NewRows(columns()), "done", 1, nil))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT * FROM lead_action_runs WHERE workspace_id = $1 AND idempotency_key = $2")).
		WithArgs(workspaceID, "key-1").WillReturnRows(sqlmock.NewRows(columns()))

	r, err := s.Get(context.Background(), workspaceID, runID)
	if err != nil || r.ID != runID || r.Status != leadaction.StatusDone {
		t.Fatalf("Get = %+v, %v", r, err)
	}
	if _, err := s.FindByKey(context.Background(), workspaceID, "key-1"); !errors.Is(err, leadaction.ErrRunNotFound) {
		t.Fatalf("FindByKey = %v", err)
	}
	if _, err := s.Get(context.Background(), workspaceID, "not-a-uuid"); !errors.Is(err, leadaction.ErrRunNotFound) {
		t.Fatalf("a malformed id = %v", err)
	}
}

func TestClaimableAndStalled(t *testing.T) {
	db, mock, _ := mockDB(t)
	s := NewStore(db)
	stale := at.Add(-leadaction.StaleAfter)

	mock.ExpectQuery(regexp.QuoteMeta("SELECT id::text FROM lead_action_runs WHERE attempts < $1")).
		WithArgs(leadaction.MaxAttempts, at, stale, 20).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(runID))
	mock.ExpectQuery(regexp.QuoteMeta("WITH stalled AS (UPDATE lead_action_runs SET status = 'failed', failure_code = 'stalled'")+
		".*"+regexp.QuoteMeta("RETURNING action) SELECT action, count(*) AS runs FROM stalled GROUP BY action")).
		WithArgs(at, at, leadaction.MaxAttempts, at, stale).
		WillReturnRows(sqlmock.NewRows([]string{"action", "runs"}).AddRow("classify", 2).AddRow("block", 1))

	ids, err := s.Claimable(context.Background(), at, 20)
	if err != nil || len(ids) != 1 {
		t.Fatalf("Claimable = %v, %v", ids, err)
	}
	stalled, err := s.FailStalled(context.Background(), at)
	if err != nil || !reflect.DeepEqual(stalled, map[leadaction.Action]int{leadaction.ActionClassify: 2, leadaction.ActionBlock: 1}) {
		t.Fatalf("FailStalled = %v, %v", stalled, err)
	}
	if got := strings.Count(failStalledSQL, "?"); got != 5 {
		t.Fatalf("stalled binds %d placeholders", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRowRoundTripKeepsTheParams(t *testing.T) {
	owner := ""
	run := &leadaction.Run{ID: runID, WorkspaceID: workspaceID, ActorID: actorID, Action: leadaction.ActionAssignOwner,
		Params: leadaction.Params{OwnerID: &owner}, Status: leadaction.StatusQueued, Phase: leadaction.PhaseEdit,
		Result: leadaction.Result{Skipped: map[leadaction.SkipReason]int{leadaction.SkipUnchanged: 2}}, CreatedAt: at, UpdatedAt: at}
	row, err := rowOf(run)
	if err != nil {
		t.Fatal(err)
	}
	back, err := runOf(*row)
	if err != nil {
		t.Fatal(err)
	}
	if back.Params.OwnerID == nil || *back.Params.OwnerID != "" || back.Result.Skipped[leadaction.SkipUnchanged] != 2 {
		t.Fatalf("round trip = %+v", back)
	}
	var params map[string]any
	_ = json.Unmarshal(row.Params, &params)
	if _, ok := params["ownerId"]; !ok {
		t.Fatalf("an empty owner must stay in the stored params: %s", row.Params)
	}
}
