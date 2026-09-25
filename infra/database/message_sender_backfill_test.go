package database

import (
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type legacyMessage struct {
	entryType   string
	messageType string
	direction   string
	from        string
	sentVia     string
	entryID     string
}

func senderBackfillTx(t *testing.T) *gorm.DB {
	t.Helper()
	tx := repairTx(t)
	for _, ddl := range []string{
		`ALTER TABLE conversation_messages ADD COLUMN IF NOT EXISTS sender_kind varchar(16) NOT NULL DEFAULT 'unknown'`,
		`ALTER TABLE conversation_messages ADD COLUMN IF NOT EXISTS sender_id varchar(120) NOT NULL DEFAULT ''`,
		`UPDATE conversation_messages SET sender_kind = 'system' WHERE sender_kind = 'unknown'`,
	} {
		if err := tx.Exec(ddl).Error; err != nil {
			t.Fatalf("prepare: %v", err)
		}
	}
	return tx
}

func seedLegacyMessage(t *testing.T, tx *gorm.DB, m legacyMessage) string {
	t.Helper()
	id := uuid.NewString()
	entryID := m.entryID
	if entryID == "" {
		entryID = uuid.NewString()
	}
	if err := tx.Exec(`
		INSERT INTO conversation_messages
			(id, entry_id, entry_type, channel, message_type, direction, from_participant, to_participant, text, sent_via, sender_kind, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, '', 'x', ?, 'unknown', NOW(), NOW())`,
		id, entryID, m.entryType, m.entryType, m.messageType, m.direction, m.from, m.sentVia).Error; err != nil {
		t.Fatalf("seed %+v: %v", m, err)
	}
	return id
}

func existingID(t *testing.T, tx *gorm.DB, table string) string {
	t.Helper()
	var id string
	tx.Raw("SELECT id::text FROM " + table + " LIMIT 1").Scan(&id)
	if id == "" {
		t.Skipf("no %s row to attribute to", table)
	}
	return id
}

func senderOf(t *testing.T, tx *gorm.DB, id string) (string, string) {
	t.Helper()
	var row struct {
		SenderKind string
		SenderID   string
	}
	if err := tx.Raw(`SELECT sender_kind, sender_id FROM conversation_messages WHERE id = ?`, id).Scan(&row).Error; err != nil {
		t.Fatalf("read %s: %v", id, err)
	}
	return row.SenderKind, row.SenderID
}

func TestBackfillAttributesLegacyMessages(t *testing.T) {
	tx := senderBackfillTx(t)
	userID := existingID(t, tx, "users")
	agentID := existingID(t, tx, "agents")
	accountUUID := uuid.NewString()

	cases := []struct {
		name     string
		message  legacyMessage
		wantKind string
		wantID   string
	}{
		{"customer text", legacyMessage{"whatsapp", "user_message", "INBOUND", "5511999", "", ""}, "contact", "5511999"},
		{"customer text before directions existed", legacyMessage{"whatsapp", "user_message", "", "5511888", "", ""}, "contact", "5511888"},
		{"customer call stored as outbound", legacyMessage{"whatsapp", "call_received", "OUTBOUND", "5511777", "", ""}, "contact", "5511777"},
		{"customer granting calls", legacyMessage{"whatsapp", "call_permission_granted", "OUTBOUND", "5511666", "", ""}, "contact", "5511666"},
		{"operator text", legacyMessage{"whatsapp", "operator", "OUTBOUND", userID, "", ""}, "human", userID},
		{"operator asking to call", legacyMessage{"whatsapp", "call_permission_request", "OUTBOUND", userID, "", ""}, "human", userID},
		{"agent reply through a channel", legacyMessage{"instagram", "ai_response", "OUTBOUND", agentID, "", ""}, "ai", "ai:" + agentID},
		{"ai reply from the business number", legacyMessage{"whatsapp", "ai_response", "OUTBOUND", "5511000", "", ""}, "ai", ""},
		{"tool call", legacyMessage{"whatsapp", "tool_call", "OUTBOUND", "system", "", ""}, "ai", ""},
		{"system note", legacyMessage{"whatsapp", "system", "OUTBOUND", "", "", ""}, "system", ""},
		{"whatsapp business app", legacyMessage{"whatsapp", "operator", "OUTBOUND", "5511000", "business_app", ""}, "external", ""},
		{"instagram app echo", legacyMessage{"instagram", "operator", "OUTBOUND", "17841400000", "", ""}, "external", ""},
		{"telegram outbound", legacyMessage{"telegram", "operator", "OUTBOUND", "7000000", "", ""}, "external", ""},
		{"official template with no person", legacyMessage{"whatsapp", "template", "OUTBOUND", "", "", ""}, "campaign", ""},
		{"unofficial campaign send", legacyMessage{"unofficial_whatsapp", "operator", "OUTBOUND", "", "", ""}, "campaign", ""},
		{"account uuid is not a person", legacyMessage{"unofficial_whatsapp", "media", "OUTBOUND", accountUUID, "", ""}, "legacy", ""},
		{"phone send on unofficial", legacyMessage{"unofficial_whatsapp", "user_message", "OUTBOUND", "5511000", "", ""}, "external", ""},
	}

	ids := make([]string, len(cases))
	for i, tc := range cases {
		ids[i] = seedLegacyMessage(t, tx, tc.message)
	}

	for {
		n, err := BackfillMessageSendersBatch(tx, 5)
		if err != nil {
			t.Fatalf("backfill: %v", err)
		}
		if n == 0 {
			break
		}
	}

	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			kind, id := senderOf(t, tx, ids[i])
			if kind != tc.wantKind || id != tc.wantID {
				t.Fatalf("got %s %q, want %s %q", kind, id, tc.wantKind, tc.wantID)
			}
		})
	}
}

func TestBackfillNamesTheCampaignOfAnUnofficialCampaignConversation(t *testing.T) {
	tx := senderBackfillTx(t)
	var conversationID, campaignID string
	tx.Raw(`SELECT id::text, campaign_id FROM unofficial_whatsapp_conversations WHERE campaign_id <> '' LIMIT 1`).Row().Scan(&conversationID, &campaignID)
	if conversationID == "" {
		t.Skip("no unofficial campaign conversation to attribute to")
	}

	id := seedLegacyMessage(t, tx, legacyMessage{"unofficial_whatsapp", "operator", "OUTBOUND", "", "", conversationID})
	if _, err := BackfillMessageSendersBatch(tx, 100); err != nil {
		t.Fatalf("backfill: %v", err)
	}

	kind, sender := senderOf(t, tx, id)
	if kind != "campaign" || sender != "campaign:"+campaignID {
		t.Fatalf("got %s %q, want campaign campaign:%s", kind, sender, campaignID)
	}
}

func TestBackfillLeavesAttributedMessagesAlone(t *testing.T) {
	tx := senderBackfillTx(t)
	id := seedLegacyMessage(t, tx, legacyMessage{"whatsapp", "user_message", "INBOUND", "5511999", "", ""})
	if err := tx.Exec(`UPDATE conversation_messages SET sender_kind = 'workflow', sender_id = 'workflow:wf-1' WHERE id = ?`, id).Error; err != nil {
		t.Fatal(err)
	}

	if _, err := BackfillMessageSendersBatch(tx, 100); err != nil {
		t.Fatalf("backfill: %v", err)
	}

	if kind, sender := senderOf(t, tx, id); kind != "workflow" || sender != "workflow:wf-1" {
		t.Fatalf("an attributed message was rewritten to %s %q", kind, sender)
	}
}
