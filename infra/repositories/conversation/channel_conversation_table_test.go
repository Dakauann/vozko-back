package conversation_repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"vozko/domain/conversation"
	"vozko/infra/database/schema"
)

var errTestNotFound = errors.New("test conversation not found")

func testTable(t *testing.T) (ChannelConversationTable, sqlmock.Sqlmock, func()) {
	db, mock, sqlDB := newStatusDB(t)
	return ChannelConversationTable{
		DB:              db,
		NewModel:        func() any { return &schema.FacebookConversation{} },
		ContainerColumn: "page_id",
		NotFound:        errTestNotFound,
	}, mock, func() { sqlDB.Close() }
}

func TestTouchClocksIsMonotonic(t *testing.T) {
	table, mock, done := testTable(t)
	defer done()
	mock.ExpectExec(`UPDATE "facebook_conversations" SET "last_customer_message_at"=GREATEST\(COALESCE\(last_customer_message_at, .*\), .*\),"last_message_at"=GREATEST`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	if err := table.RecordInbound(context.Background(), "c1", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestMissingRowReportsTheChannelNotFound(t *testing.T) {
	table, mock, done := testTable(t)
	defer done()
	mock.ExpectExec(`UPDATE "facebook_conversations"`).WillReturnResult(sqlmock.NewResult(0, 0))
	if err := table.SetStatus(context.Background(), "c1", conversation.StatusWrite{}); !errors.Is(err, errTestNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestCountByStatusScopesToTheContainerColumn(t *testing.T) {
	table, mock, done := testTable(t)
	defer done()
	mock.ExpectQuery(`SELECT COALESCE\(NULLIF\(conversation_status, ''\), 'new'\) AS status, COUNT\(\*\) AS cnt FROM "facebook_conversations" WHERE deleted_at IS NULL AND last_message_at IS NOT NULL AND page_id = \$1`).
		WithArgs("page-1").
		WillReturnRows(sqlmock.NewRows([]string{"status", "cnt"}).AddRow("new", 3).AddRow("closed", 1))
	got, err := table.CountByStatus(context.Background(), "ws", "page-1")
	if err != nil || got["new"] != 3 || got["closed"] != 1 {
		t.Fatalf("got %v, %v", got, err)
	}
}

func TestCountByStatusWithoutScopeIsEmpty(t *testing.T) {
	table, _, done := testTable(t)
	defer done()
	got, err := table.CountByStatus(context.Background(), "", "")
	if err != nil || len(got) != 0 {
		t.Fatalf("got %v, %v", got, err)
	}
}

func TestWorkspaceIDForMissingEntry(t *testing.T) {
	table, mock, done := testTable(t)
	defer done()
	mock.ExpectQuery(`SELECT "workspace_id" FROM "facebook_conversations"`).
		WillReturnRows(sqlmock.NewRows([]string{"workspace_id"}))
	if _, err := table.WorkspaceIDForEntry(context.Background(), "c1"); !errors.Is(err, errTestNotFound) {
		t.Fatalf("got %v", err)
	}
}
