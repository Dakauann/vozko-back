package leadexit_repository

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"vozko/domain/buttonaction"
	"vozko/domain/leadexit"
)

const workspaceA = "6f1d1c52-2a6c-4b8e-9f43-1c1f5b7d2a01"

func newMockDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB, PreferSimpleProtocol: true}), &gorm.Config{SkipDefaultTransaction: true})
	if err != nil {
		t.Fatal(err)
	}
	return db, mock
}

func TestEveryStatementBindsAsManyArgumentsAsItHasPlaceholders(t *testing.T) {
	cases := map[string]struct {
		sql  string
		args int
	}{
		"claim run":  {claimRunSQL, 9},
		"select run": {selectRunSQL, 2},
		"finish run": {finishRunSQL, 8},
	}
	for name, tc := range cases {
		if got := strings.Count(tc.sql, "?"); got != tc.args {
			t.Errorf("%s: %d placeholders, the call binds %d", name, got, tc.args)
		}
	}
}

func TestAStoreRefusesAWorkspaceThatIsNotAnID(t *testing.T) {
	db, _ := newMockDB(t)
	if _, err := NewRunStore(db).Claim(context.Background(), leadexit.Run{WorkspaceID: "x", InboundMessageID: "wamid.1"}); !errors.Is(err, leadexit.ErrWorkspaceRequired) {
		t.Fatalf("claim: %v", err)
	}
}

func TestARunThatIsNotClaimedReturnsThePreviousOne(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectQuery(`INSERT INTO lead_exit_runs`).WillReturnRows(sqlmock.NewRows([]string{"inbound_message_id"}))
	mock.ExpectQuery(`FROM lead_exit_runs`).WithArgs(workspaceA, "wamid.1").
		WillReturnRows(sqlmock.NewRows([]string{"workspace_id", "inbound_message_id", "kind", "exit_trigger", "outcome", "updated_at"}).
			AddRow(workspaceA, "wamid.1", "opt_out", "button", "changed", time.Now()))

	claim, err := NewRunStore(db).Claim(context.Background(), leadexit.Run{WorkspaceID: workspaceA, InboundMessageID: "wamid.1", Kind: buttonaction.KindOptOut})

	if err != nil || claim.Claimed || claim.Previous.Outcome != leadexit.OutcomeChanged || claim.Previous.Kind != buttonaction.KindOptOut || claim.Previous.Trigger != leadexit.TriggerButton {
		t.Fatalf("claim = %+v, %v", claim, err)
	}
}

func TestFinishingARunThatWasNeverClaimedIsAnError(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectExec(`UPDATE lead_exit_runs SET`).WillReturnResult(sqlmock.NewResult(0, 0))

	err := NewRunStore(db).Finish(context.Background(), leadexit.Run{WorkspaceID: workspaceA, InboundMessageID: "wamid.1", Outcome: leadexit.OutcomeChanged})

	if err == nil {
		t.Fatal("want an error")
	}
}
