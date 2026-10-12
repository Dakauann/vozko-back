package aichat_repository

import (
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"vozko/domain/aichat"
)

func mockThreads(t *testing.T) (*threadRepository, sqlmock.Sqlmock) {
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
	return &threadRepository{db: db}, mock
}

func TestTouchStoresTheReasoningLevelNextToTheModel(t *testing.T) {
	repo, mock := mockThreads(t)
	at := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "ai_chat_threads" SET "last_message_at"=\$1,"model"=\$2,"reasoning_level"=\$3,"updated_at"=\$4 WHERE id = \$5`).
		WithArgs(at, "lab/thinker", "high", sqlmock.AnyArg(), "t1").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	if err := repo.Touch("t1", at, "lab/thinker", "high"); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestTouchClearsTheLevelWhenTheModelChangesToItsDefault(t *testing.T) {
	repo, mock := mockThreads(t)
	at := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "ai_chat_threads" SET "last_message_at"=\$1,"model"=\$2,"reasoning_level"=\$3,"updated_at"=\$4 WHERE id = \$5`).
		WithArgs(at, "lab/plain", "", sqlmock.AnyArg(), "t1").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	if err := repo.Touch("t1", at, "lab/plain", ""); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestTouchWithoutAModelKeepsModelAndLevel(t *testing.T) {
	repo, mock := mockThreads(t)
	at := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "ai_chat_threads" SET "last_message_at"=\$1,"updated_at"=\$2 WHERE id = \$3`).
		WithArgs(at, sqlmock.AnyArg(), "t1").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	if err := repo.Touch("t1", at, "", "high"); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestThreadMappingCarriesTheReasoningLevel(t *testing.T) {
	fixture := aichat.Thread{ID: "t1", Model: "lab/thinker", ReasoningLevel: "medium"}
	rec := toThreadSchema(toThreadDomain(toThreadSchema(&fixture)))
	if rec.ReasoningLevel != "medium" {
		t.Fatalf("reasoning level = %q, want medium", rec.ReasoningLevel)
	}
}
