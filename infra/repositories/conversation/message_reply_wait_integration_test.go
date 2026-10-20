package conversation_repository

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"vozko/domain/conversation"
	"vozko/domain/shared"
	"vozko/infra/database/schema"
	"vozko/infra/repositories/repotest"
)

func replyWaitDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := repotest.IsolatedDB(t, "reply_wait_test",
		&schema.ConversationMessage{},
		&schema.WhatsAppCampaignEntry{},
		&schema.InstagramConversation{},
		&schema.FacebookConversation{},
		&schema.TelegramConversation{},
		&schema.WebchatConversation{},
		&schema.MercadoLivreConversation{},
		&schema.TikTokConversation{},
		&schema.UnofficialWhatsAppConversation{},
	)
	var constraints []struct {
		Table string `gorm:"column:table_name"`
		Name  string `gorm:"column:constraint_name"`
	}
	if err := db.Raw(`SELECT table_name, constraint_name FROM information_schema.table_constraints
		WHERE constraint_type = 'FOREIGN KEY' AND table_schema = current_schema()`).Scan(&constraints).Error; err != nil {
		t.Fatalf("list foreign keys: %v", err)
	}
	for _, c := range constraints {
		if err := db.Exec(`ALTER TABLE "` + c.Table + `" DROP CONSTRAINT "` + c.Name + `"`).Error; err != nil {
			t.Fatalf("drop %s: %v", c.Name, err)
		}
	}
	return db
}

func seedChannelConversation(t *testing.T, db *gorm.DB, ch channelQuery, status conversation.ConversationStatus) string {
	t.Helper()
	id := uuid.NewString()
	inserts := map[shared.EntryType]string{
		shared.EntryTypeWhatsApp:           `INSERT INTO whatsapp_campaign_entries (id, campaign_id, lead_id, conversation_status) VALUES (?, gen_random_uuid(), gen_random_uuid(), ?)`,
		shared.EntryTypeInstagram:          `INSERT INTO instagram_conversations (id, workspace_id, ig_account_id, contact_id, conversation_status) VALUES (?, gen_random_uuid(), gen_random_uuid(), gen_random_uuid(), ?)`,
		shared.EntryTypeFacebook:           `INSERT INTO facebook_conversations (id, workspace_id, page_id, contact_id, conversation_status) VALUES (?, gen_random_uuid(), gen_random_uuid(), gen_random_uuid(), ?)`,
		shared.EntryTypeTelegram:           `INSERT INTO telegram_conversations (id, workspace_id, account_id, contact_id, tg_chat_id, conversation_status) VALUES (?, gen_random_uuid(), gen_random_uuid(), gen_random_uuid(), 1, ?)`,
		shared.EntryTypeWebchat:            `INSERT INTO webchat_conversations (id, workspace_id, widget_id, visitor_id, conversation_status) VALUES (?, gen_random_uuid(), gen_random_uuid(), gen_random_uuid(), ?)`,
		shared.EntryTypeUnofficialWhatsApp: `INSERT INTO unofficial_whatsapp_conversations (id, workspace_id, instance_id, contact_id, chat_id, conversation_status) VALUES (?, gen_random_uuid(), gen_random_uuid(), gen_random_uuid(), 'chat', ?)`,
		shared.EntryTypeMercadoLivre:       `INSERT INTO mercadolivre_conversations (id, workspace_id, account_id, contact_id, pack_key, conversation_status) VALUES (?, gen_random_uuid(), gen_random_uuid(), gen_random_uuid(), '2000000001', ?)`,
		shared.EntryTypeTikTok:             `INSERT INTO tiktok_conversations (id, workspace_id, account_id, contact_id, conversation_id, conversation_status) VALUES (?, gen_random_uuid(), gen_random_uuid(), gen_random_uuid(), 'S:1:0:wait', ?)`,
	}
	insert, ok := inserts[ch.EntryType]
	if !ok {
		t.Fatalf("no seed for %s: a channel was added to the inbox without a reply wait test", ch.EntryType)
	}
	if err := db.Exec(insert, id, string(status)).Error; err != nil {
		t.Fatalf("seed %s: %v", ch.EntryTable, err)
	}
	return id
}

func awaitingReplySince(t *testing.T, db *gorm.DB, ch channelQuery, id string) *time.Time {
	t.Helper()
	var row struct {
		At *time.Time `gorm:"column:awaiting_reply_since"`
	}
	if err := db.Raw(`SELECT awaiting_reply_since FROM `+ch.EntryTable+` WHERE id = ?`, id).Scan(&row).Error; err != nil {
		t.Fatalf("read awaiting_reply_since: %v", err)
	}
	return row.At
}

func TestTheStoredWaitFollowsTheConversationOnEveryChannelAgainstPostgres(t *testing.T) {
	db := replyWaitDB(t)
	repo := &repository{db: db}
	base := time.Date(2026, 10, 19, 9, 0, 0, 0, time.UTC)
	at := func(minutes int) time.Time { return base.Add(time.Duration(minutes) * time.Minute) }

	steps := []struct {
		name   string
		sender conversation.SentBy
		minute int
		want   *int
	}{
		{"the first customer message starts the wait", conversation.SentByContact("5511"), 0, ptrInt(0)},
		{"more customer messages keep the first one", conversation.SentByContact("5511"), 20, ptrInt(0)},
		{"a campaign blast is not an answer", conversation.SentByCampaign("camp-1"), 30, ptrInt(0)},
		{"a system notice is not an answer", conversation.SentBySystem(), 35, ptrInt(0)},
		{"an operator reply ends the wait", conversation.SentByPerson(uuid.NewString()), 40, nil},
		{"a late customer message older than the reply was already answered", conversation.SentByContact("5511"), 39, nil},
		{"the next customer message starts a new wait", conversation.SentByContact("5511"), 50, ptrInt(50)},
		{"a late reply older than the wait answers nothing", conversation.SentByAI("agent-1"), 45, ptrInt(50)},
		{"a customer message delivered late moves the wait back", conversation.SentByContact("5511"), 48, ptrInt(48)},
		{"an AI reply ends the wait", conversation.SentByAI("agent-1"), 55, nil},
		{"customer again", conversation.SentByContact("5511"), 60, ptrInt(60)},
		{"and once more, before anyone answers", conversation.SentByContact("5511"), 70, ptrInt(60)},
		{"a reply recorded late between them leaves the newer message waiting", conversation.SentByWorkflow("wf-1"), 65, ptrInt(70)},
		{"a reply typed on the phone answers", conversation.SentExternally(), 75, nil},
	}

	for _, ch := range channelQueries {
		t.Run(string(ch.EntryType), func(t *testing.T) {
			id := seedChannelConversation(t, db, ch, conversation.ConversationStatusOngoing)
			for _, step := range steps {
				msg := &conversation.Message{
					ID: uuid.NewString(), EntryID: id, EntryType: ch.EntryType,
					MessageType: conversation.MessageTypeUserMessage, From: "x", Text: step.name,
					SentBy: step.sender, CreatedAt: at(step.minute), UpdatedAt: at(step.minute),
				}
				if err := repo.Create(msg); err != nil {
					t.Fatalf("%s: create: %v", step.name, err)
				}
				got := awaitingReplySince(t, db, ch, id)
				switch {
				case step.want == nil && got != nil:
					t.Fatalf("%s: waiting since %v, want nobody waiting", step.name, got.UTC())
				case step.want != nil && (got == nil || !got.Equal(at(*step.want))):
					t.Fatalf("%s: waiting since %v, want %v", step.name, got, at(*step.want))
				}
			}
		})
	}
}

func ptrInt(v int) *int { return &v }
