package conversation_repository

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"vozko/domain/shared"
	"vozko/infra/database/schema"
	"vozko/infra/repositories/repotest"
)

func TestLastMessageCarriesItsSenderAgainstPostgres(t *testing.T) {
	db := repotest.IsolatedDB(t, "last_sender_test",
		&schema.ConversationMessage{},
		&schema.UnofficialWhatsAppInstance{},
		&schema.UnofficialWhatsAppContact{},
		&schema.UnofficialWhatsAppConversation{},
		&schema.UnofficialWhatsAppCampaign{},
	)
	repo := &repository{db: db}

	instanceID, conversationID := uuid.NewString(), uuid.NewString()
	exec := func(sql string, args ...interface{}) {
		if err := db.Exec(sql, args...).Error; err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	exec(`INSERT INTO unofficial_whatsapp_instances (id, workspace_id, server_id, provider_instance_id, delivery_token_hash) VALUES (?, ?, ?, 'p-1', 'h-1')`,
		instanceID, uuid.NewString(), uuid.NewString())
	exec(`INSERT INTO unofficial_whatsapp_conversations (id, workspace_id, instance_id, contact_id, chat_id) VALUES (?, ?, ?, ?, 'grupo@g.us')`,
		conversationID, uuid.NewString(), instanceID, uuid.NewString())
	at := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	exec(`INSERT INTO conversation_messages (id, entry_id, entry_type, message_type, from_participant, text, sender_kind, sender_id, created_at, updated_at)
		VALUES (?, ?, ?, 'user_message', '+5511900000001', 'antes', 'contact', '+5511900000001', ?, ?),
		       (?, ?, ?, 'user_message', '+5511900000002', 'Olá', 'contact', '+5511900000002', ?, ?)`,
		uuid.NewString(), conversationID, string(shared.EntryTypeUnofficialWhatsApp), at, at,
		uuid.NewString(), conversationID, string(shared.EntryTypeUnofficialWhatsApp), at.Add(time.Minute), at.Add(time.Minute))

	single, err := repo.GetEntryLastMessage(conversationID, shared.EntryTypeUnofficialWhatsApp)
	if err != nil || single == nil {
		t.Fatalf("GetEntryLastMessage = %+v, %v", single, err)
	}
	if single.LastMessageSenderKind != "contact" || single.LastMessageSenderID != "+5511900000002" {
		t.Errorf("single entry sender = %q/%q, want contact/+5511900000002", single.LastMessageSenderKind, single.LastMessageSenderID)
	}

	page, _, err := repo.GetEntriesWithMessages("", []string{conversationID}, shared.EntryTypeUnofficialWhatsApp, 1, 10, "")
	if err != nil || len(page) != 1 {
		t.Fatalf("GetEntriesWithMessages = %+v, %v", page, err)
	}
	if page[0].LastMessageSenderKind != "contact" || page[0].LastMessageSenderID != "+5511900000002" {
		t.Errorf("page sender = %q/%q, want contact/+5511900000002", page[0].LastMessageSenderKind, page[0].LastMessageSenderID)
	}
	if page[0].BusinessPhoneID != instanceID {
		t.Errorf("account = %q, want the instance %q the group authors are scoped to", page[0].BusinessPhoneID, instanceID)
	}

	exec(`INSERT INTO conversation_messages (id, entry_id, entry_type, message_type, from_participant, text, sender_kind, sender_id, created_at, updated_at, deleted_at)
		VALUES (?, ?, ?, 'user_message', '+5511900000003', 'apagada', 'contact', '+5511900000003', ?, ?, ?)`,
		uuid.NewString(), conversationID, string(shared.EntryTypeUnofficialWhatsApp), at.Add(2*time.Minute), at.Add(2*time.Minute), at.Add(3*time.Minute))
	afterDelete, err := repo.GetEntryLastMessage(conversationID, shared.EntryTypeUnofficialWhatsApp)
	if err != nil || afterDelete == nil || afterDelete.LastMessageText != "Olá" {
		t.Fatalf("last message with a deleted newer one = %+v, %v, want the newest message that still exists", afterDelete, err)
	}
}
