package conversation_repository

import (
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"vozko/domain/conversation"
)

func clockMessage(sentBy conversation.SentBy) *conversation.Message {
	return &conversation.Message{
		ID: "msg-1", EntryID: "entry-1", EntryType: "unofficial_whatsapp",
		MessageType: conversation.MessageTypeMedia, From: "x", Text: "catalogo.pdf",
		SentBy: sentBy, CreatedAt: time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC),
	}
}

func TestWorkflowMediaMovesTheBusinessClockNotTheCustomers(t *testing.T) {
	db, mock, sqlDB := newStatusDB(t)
	defer sqlDB.Close()

	mock.ExpectExec(`INSERT INTO "conversation_messages"`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE unofficial_whatsapp_conversations SET last_message_at = .*, last_agent_message_at = .* WHERE id = \$\d+$`).
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := NewRepository(db).Create(clockMessage(conversation.SentByWorkflow("wf-1"))); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestAContactMessageMovesTheCustomerClock(t *testing.T) {
	db, mock, sqlDB := newStatusDB(t)
	defer sqlDB.Close()

	mock.ExpectExec(`INSERT INTO "conversation_messages"`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE unofficial_whatsapp_conversations SET last_message_at = .*, last_customer_message_at = .* WHERE id = \$\d+$`).
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := NewRepository(db).Create(clockMessage(conversation.SentByContact("5511"))); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
