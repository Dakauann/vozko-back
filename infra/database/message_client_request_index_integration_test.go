package database

import (
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"vozko/infra/database/schema"
	"vozko/infra/repositories/repotest"
)

func insertRequestedMessage(db *gorm.DB, entryType, entryID string, requestID *string) error {
	return db.Exec(`INSERT INTO conversation_messages (id, entry_id, entry_type, text, client_request_id, created_at, updated_at)
		VALUES (?, ?, ?, 'oi', ?, NOW(), NOW())`, uuid.NewString(), entryID, entryType, requestID).Error
}

func TestTheClientRequestIndexKeepsOneMessagePerRequestAgainstPostgres(t *testing.T) {
	db := repotest.IsolatedDB(t, "msg_client_req", &schema.ConversationMessage{})
	if err := createIndexConcurrently(db, declaredConcurrentIndex(t, MessageClientRequestIndex)); err != nil {
		t.Fatalf("createIndexConcurrently() error = %v", err)
	}
	if err := createIndexConcurrently(db, declaredConcurrentIndex(t, MessageClientRequestIndex)); err != nil {
		t.Fatalf("a second boot must be a no op, got %v", err)
	}

	entryID, request := uuid.NewString(), "req-1"
	if err := insertRequestedMessage(db, "whatsapp", entryID, &request); err != nil {
		t.Fatal(err)
	}
	if err := insertRequestedMessage(db, "whatsapp", entryID, &request); !IsUniqueViolationOf(err, MessageClientRequestIndex) {
		t.Fatalf("a replayed request stored a second message, err = %v", err)
	}
	if err := insertRequestedMessage(db, "whatsapp", uuid.NewString(), &request); err != nil {
		t.Fatalf("the same request id on another conversation is a different send: %v", err)
	}
	if err := insertRequestedMessage(db, "telegram", entryID, &request); err != nil {
		t.Fatalf("the same request id on another channel is a different send: %v", err)
	}
	for i := 0; i < 2; i++ {
		if err := insertRequestedMessage(db, "whatsapp", entryID, nil); err != nil {
			t.Fatalf("messages without a request id are never deduplicated: %v", err)
		}
	}
}
