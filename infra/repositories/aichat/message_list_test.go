package aichat_repository

import (
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"vozko/domain/aichat"
)

func TestListingWithoutALimitReturnsTheWholeThread(t *testing.T) {
	threads, mock := mockThreads(t)
	repo := &messageRepository{db: threads.db}
	mock.ExpectQuery(`SELECT count\(\*\) FROM "ai_chat_messages"`).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(2))
	mock.ExpectQuery(`SELECT \* FROM "ai_chat_messages" WHERE thread_id = \$1 AND "ai_chat_messages"."deleted_at" IS NULL ORDER BY created_at ASC$`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "role"}).AddRow("m1", "user").AddRow("m2", "assistant"))
	msgs, _, err := repo.ListByThread(aichat.ListMessagesInput{ThreadID: "t1"})
	if err != nil || len(msgs) != 2 {
		t.Fatalf("listed %d messages, err %v; a zero limit must mean no limit", len(msgs), err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestListingAfterACheckpointKeepsOnlyNewerMessages(t *testing.T) {
	threads, mock := mockThreads(t)
	repo := &messageRepository{db: threads.db}
	at := time.Date(2026, 10, 12, 15, 0, 0, 0, time.UTC)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "ai_chat_messages" WHERE thread_id = $1 AND created_at > $2`)).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	mock.ExpectQuery(regexp.QuoteMeta(`WHERE thread_id = $1 AND created_at > $2`)).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("m3"))
	msgs, _, err := repo.ListByThread(aichat.ListMessagesInput{ThreadID: "t1", After: &at})
	if err != nil || len(msgs) != 1 {
		t.Fatalf("listed %d, err %v", len(msgs), err)
	}
}
