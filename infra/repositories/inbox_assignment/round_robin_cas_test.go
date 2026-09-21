package inbox_assignment_repository

import (
	"database/sql"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	ia "vozko/domain/inbox_assignment"
)

func newRRDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock, *sql.DB) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatal(err)
	}
	mock.MatchExpectationsInOrder(false)
	db, err := gorm.Open(
		postgres.New(postgres.Config{
			Conn:                 sqlDB,
			PreferSimpleProtocol: true,
			WithoutReturning:     true,
		}),
		&gorm.Config{SkipDefaultTransaction: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	return db, mock, sqlDB
}

func rrState() *ia.RoundRobinState {
	return &ia.RoundRobinState{
		WorkspaceID:        "ws-1",
		BusinessPhoneID:    "phone-1",
		DepartmentID:       "",
		LastAssignedUserID: "next-agent",
	}
}

func TestCompareAndSwap_GuardsOnTheExpectedPointer(t *testing.T) {
	db, mock, sqlDB := newRRDB(t)
	defer sqlDB.Close()

	mock.ExpectExec(`UPDATE "inbox_round_robin_state" SET .*` +
		`WHERE workspace_id = \$\d+ AND business_phone_id = \$\d+ AND department_id = \$\d+ ` +
		`AND last_assigned_user_id = \$\d+`).
		WillReturnResult(sqlmock.NewResult(0, 1))

	ok, err := New(db).CompareAndSwapRoundRobinState(rrState(), "previous-agent")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("a matching pointer must be claimed")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestCompareAndSwap_ReportsALostRace(t *testing.T) {
	db, mock, sqlDB := newRRDB(t)
	defer sqlDB.Close()

	mock.ExpectExec(`UPDATE "inbox_round_robin_state"`).
		WillReturnResult(sqlmock.NewResult(0, 0))

	ok, err := New(db).CompareAndSwapRoundRobinState(rrState(), "previous-agent")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("a pointer that moved under us must not be reported as claimed")
	}
}

func TestCompareAndSwap_FirstTurnInsertsAndCanLose(t *testing.T) {
	for name, tc := range map[string]struct {
		inserted int64
		want     bool
	}{
		"we inserted it":                {1, true},
		"somebody else got there first": {0, false},
	} {
		t.Run(name, func(t *testing.T) {
			db, mock, sqlDB := newRRDB(t)
			defer sqlDB.Close()

			mock.ExpectExec(`UPDATE "inbox_round_robin_state"`).
				WillReturnResult(sqlmock.NewResult(0, 0))
			mock.ExpectExec(`INSERT INTO "inbox_round_robin_state" .*ON CONFLICT .*DO NOTHING`).
				WillReturnResult(sqlmock.NewResult(0, tc.inserted))

			ok, err := New(db).CompareAndSwapRoundRobinState(rrState(), "")
			if err != nil {
				t.Fatal(err)
			}
			if ok != tc.want {
				t.Fatalf("got %v want %v", ok, tc.want)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
