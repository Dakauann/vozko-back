package emailsending_repository

import (
	"context"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"

	es "vozko/domain/emailsending"
)

func TestListByRunSQLShape(t *testing.T) {
	db, mock := mockDB(t)
	repo := NewRunMessages(db)
	query := regexp.QuoteMeta(`SELECT * FROM "email_messages" WHERE workspace_id = $1 AND run_kind = $2 AND run_id = $3 ORDER BY created_at ASC`)
	mock.ExpectQuery(query).WithArgs("ws-1", "triggered", "run-1").
		WillReturnRows(sqlmock.NewRows([]string{"id", "workspace_id", "run_kind", "run_id", "status"}).
			AddRow("m-1", "ws-1", "triggered", "run-1", "failed").
			AddRow("m-2", "ws-1", "triggered", "run-1", "sent"))
	got, err := repo.ListByRun(context.Background(), "ws-1", es.RunTriggered, "run-1")
	if err != nil || len(got) != 2 || got[0].Status != es.MessageFailed || got[1].ID != "m-2" {
		t.Fatalf("got %+v, %v", got, err)
	}
	for _, blank := range [][3]string{{"", "triggered", "run-1"}, {"ws-1", "", "run-1"}, {"ws-1", "triggered", " "}} {
		if got, err := repo.ListByRun(context.Background(), blank[0], es.RunKind(blank[1]), blank[2]); err != nil || len(got) != 0 {
			t.Fatalf("blank %v must answer nothing without a query: %+v, %v", blank, got, err)
		}
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestIntegrationListByRunIsScopedToTheWorkspace(t *testing.T) {
	db := integrationDB(t)
	messages := NewMessageRepository(db)
	runs := NewRunMessages(db)
	ctx := context.Background()
	ws, other, dom := uuid.NewString(), uuid.NewString(), uuid.NewString()
	rows := []*es.Message{
		{WorkspaceID: ws, RunKind: es.RunTriggered, RunID: "k", DomainID: dom, Address: "a@b.com", Status: es.MessageFailed},
		{WorkspaceID: ws, RunKind: es.RunTriggered, RunID: "k", DomainID: dom, Address: "a@b.com", Status: es.MessageQueued},
		{WorkspaceID: other, RunKind: es.RunTriggered, RunID: "k", DomainID: dom, Address: "a@b.com", Status: es.MessageSent},
		{WorkspaceID: ws, RunKind: es.RunReply, RunID: "k", DomainID: dom, Address: "a@b.com", Status: es.MessageSent},
	}
	for _, m := range rows {
		if err := messages.Record(ctx, m); err != nil {
			t.Fatal(err)
		}
	}
	got, err := runs.ListByRun(ctx, ws, es.RunTriggered, "k")
	if err != nil || len(got) != 2 {
		t.Fatalf("got %+v, %v", got, err)
	}
	for _, m := range got {
		if m.WorkspaceID != ws || m.RunKind != es.RunTriggered {
			t.Fatalf("leaked row %+v", m)
		}
	}
}
