package ai_attendance_repository

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	aa "vozko/domain/ai_attendance"
)

func mockDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB, PreferSimpleProtocol: true, WithoutReturning: true}), &gorm.Config{SkipDefaultTransaction: true})
	if err != nil {
		t.Fatal(err)
	}
	return db, mock
}

func TestIncrementToolCallsAddsInSQLSoConcurrentTurnsNeverLoseACount(t *testing.T) {
	db, mock := mockDB(t)
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE "ai_attendance_sessions" SET "tool_call_count"=tool_call_count + $1,"updated_at"=$2 WHERE id = $3`)).
		WithArgs(2, sqlmock.AnyArg(), "sess-1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := New(db).IncrementToolCalls("sess-1", 2); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestIncrementToolCallsIgnoresEmptyCounts(t *testing.T) {
	db, mock := mockDB(t)
	for _, count := range []int{0, -1} {
		if err := New(db).IncrementToolCalls("sess-1", count); err != nil {
			t.Fatal(err)
		}
	}
	if err := New(db).IncrementToolCalls("", 3); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestUpdateNeverRewritesTheToolCallCounterFromAStaleRead(t *testing.T) {
	db, mock := mockDB(t)
	var statement string
	mock.ExpectExec(`UPDATE "ai_attendance_sessions"`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	db.Callback().Update().After("gorm:update").Register("capture_sql", func(tx *gorm.DB) {
		statement = tx.Statement.SQL.String()
	})

	now := time.Now().UTC()
	err := New(db).Update(&aa.Session{ID: "sess-1", WorkspaceID: "ws", EntryID: "e1", EntryType: "whatsapp", AgentID: "ag", ToolCallCount: 0, StartedAt: now, CreatedAt: now, UpdatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(statement, "tool_call_count") {
		t.Fatalf("update writes tool_call_count: %s", statement)
	}
	if !strings.Contains(statement, "ai_message_count") {
		t.Fatalf("update lost the other counters: %s", statement)
	}
}
