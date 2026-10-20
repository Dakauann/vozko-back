package conversation_repository

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"vozko/domain/conversation"
	"vozko/domain/shared"
	"vozko/infra/database"
	"vozko/infra/database/schema"
	"vozko/infra/repositories/repotest"
)

func clientRequestRepo(t *testing.T) conversation.MessageRepository {
	t.Helper()
	db := repotest.IsolatedDB(t, "msg_client_req", &schema.ConversationMessage{})
	sql, ok := database.ConcurrentIndexSQL(database.MessageClientRequestIndex)
	if !ok {
		t.Fatalf("%s is not declared", database.MessageClientRequestIndex)
	}
	if err := db.Exec(sql).Error; err != nil {
		t.Fatal(err)
	}
	return NewRepository(db)
}

func operatorMessage(entryID, requestID string) *conversation.Message {
	at := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	return &conversation.Message{
		ID: uuid.NewString(), EntryID: entryID, EntryType: shared.EntryTypeWhatsApp, Channel: conversation.MessageChannelWhatsApp,
		MessageType: conversation.MessageTypeOperator, SentBy: conversation.SentByPerson("user-ana"), From: "user-ana", Text: "oi",
		ClientRequestID: requestID, CreatedAt: at, UpdatedAt: at,
	}
}

func TestAnOperatorSendIsFoundAgainByItsClientRequestAgainstPostgres(t *testing.T) {
	repo := clientRequestRepo(t)
	entryID := uuid.NewString()
	sent := operatorMessage(entryID, "req-1")
	if err := repo.Create(sent); err != nil {
		t.Fatal(err)
	}

	found, err := repo.GetByClientRequestID(shared.EntryTypeWhatsApp, entryID, "req-1")
	if err != nil {
		t.Fatal(err)
	}
	if found.ID != sent.ID || found.ClientRequestID != "req-1" {
		t.Fatalf("found = %s (%q), want %s with its request id", found.ID, found.ClientRequestID, sent.ID)
	}

	if _, err := repo.GetByClientRequestID(shared.EntryTypeWhatsApp, uuid.NewString(), "req-1"); !errors.Is(err, conversation.ErrMessageNotFound) {
		t.Fatalf("another conversation's request must not match, err = %v", err)
	}
	if _, err := repo.GetByClientRequestID(shared.EntryTypeWhatsApp, entryID, ""); !errors.Is(err, conversation.ErrMessageNotFound) {
		t.Fatalf("an empty request id matches nothing, err = %v", err)
	}
}

func TestASecondMessageForTheSameClientRequestIsRefusedAgainstPostgres(t *testing.T) {
	repo := clientRequestRepo(t)
	entryID := uuid.NewString()
	if err := repo.Create(operatorMessage(entryID, "req-1")); err != nil {
		t.Fatal(err)
	}
	if err := repo.Create(operatorMessage(entryID, "req-1")); !database.IsUniqueViolationOf(err, database.MessageClientRequestIndex) {
		t.Fatalf("a replayed request stored a second message, err = %v", err)
	}
	for i := 0; i < 2; i++ {
		if err := repo.Create(operatorMessage(entryID, "")); err != nil {
			t.Fatalf("sends without a request id are stored as before: %v", err)
		}
	}
}
