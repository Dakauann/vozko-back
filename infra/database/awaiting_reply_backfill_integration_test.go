package database

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"vozko/domain/shared"
	"vozko/infra/database/schema"
	"vozko/infra/repositories/repotest"
)

func awaitingReplyFixtures(t *testing.T) *gorm.DB {
	t.Helper()
	db := repotest.IsolatedDB(t, "awaiting_backfill",
		&schema.ConversationMessage{},
		&schema.WhatsAppCampaignEntry{},
		&schema.InstagramConversation{},
		&schema.FacebookConversation{},
		&schema.TelegramConversation{},
		&schema.WebchatConversation{},
		&schema.MercadoLivreConversation{},
		&schema.TikTokConversation{},
		&schema.UnofficialWhatsAppConversation{},
		&schema.EmailConversation{},
		&schema.RCSConversation{},
		&schema.SMSConversation{},
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

var awaitingSeedColumns = map[shared.EntryType]string{
	shared.EntryTypeWhatsApp:           `campaign_id, lead_id`,
	shared.EntryTypeInstagram:          `workspace_id, ig_account_id, contact_id`,
	shared.EntryTypeFacebook:           `workspace_id, page_id, contact_id`,
	shared.EntryTypeTelegram:           `workspace_id, account_id, contact_id`,
	shared.EntryTypeWebchat:            `workspace_id, widget_id, visitor_id`,
	shared.EntryTypeUnofficialWhatsApp: `workspace_id, instance_id, contact_id`,
	shared.EntryTypeMercadoLivre:       `workspace_id, account_id, contact_id`,
	shared.EntryTypeTikTok:             `workspace_id, account_id, contact_id`,
	shared.EntryTypeEmail:              `workspace_id, sender_id, contact_id`,
	shared.EntryTypeRCS:                `workspace_id, sender_id, contact_id`,
	shared.EntryTypeSMS:                `workspace_id, sender_id, contact_id`,
}

type awaitingSeed struct {
	status   string
	messages []awaitingSeedMessage
	stored   *time.Time
}

type awaitingSeedMessage struct {
	sender string
	at     time.Time
}

func seedAwaitingConversation(t *testing.T, db *gorm.DB, entryType shared.EntryType, table string, seed awaitingSeed) string {
	t.Helper()
	columns, ok := awaitingSeedColumns[entryType]
	if !ok {
		t.Fatalf("no seed columns for %s: a channel was added to the backfill without a test", entryType)
	}
	extra, values := "", ""
	switch entryType {
	case shared.EntryTypeTelegram:
		extra, values = ", tg_chat_id", ", 1"
	case shared.EntryTypeUnofficialWhatsApp:
		extra, values = ", chat_id", ", 'chat'"
	case shared.EntryTypeMercadoLivre:
		extra, values = ", pack_key", ", '2000000001'"
	case shared.EntryTypeTikTok:
		extra, values = ", conversation_id", ", 'S:1:0:seed'"
	case shared.EntryTypeEmail, shared.EntryTypeRCS, shared.EntryTypeSMS:
		extra, values = ", created_at, updated_at", ", NOW(), NOW()"
	}
	uuids := strings.Repeat(", gen_random_uuid()", strings.Count(columns, ",")+1)
	var customer, agent *time.Time
	for _, m := range seed.messages {
		at := m.at
		switch m.sender {
		case "contact":
			if customer == nil || at.After(*customer) {
				customer = &at
			}
		case "system":
		default:
			if agent == nil || at.After(*agent) {
				agent = &at
			}
		}
	}
	id := uuid.NewString()
	insert := fmt.Sprintf(`INSERT INTO %s (id, %s%s, conversation_status, last_message_at, last_customer_message_at, last_agent_message_at, awaiting_reply_since)
		VALUES (?%s%s, ?, NOW(), ?, ?, ?)`, table, columns, extra, uuids, values)
	if err := db.Exec(insert, id, seed.status, customer, agent, seed.stored).Error; err != nil {
		t.Fatalf("seed %s: %v", table, err)
	}
	for _, m := range seed.messages {
		if err := db.Exec(`INSERT INTO conversation_messages (id, entry_id, entry_type, message_type, from_participant, text, sender_kind, created_at, updated_at)
			VALUES (?, ?, ?, 'user_message', 'x', 'oi', ?, ?, ?)`, uuid.NewString(), id, string(entryType), m.sender, m.at, m.at).Error; err != nil {
			t.Fatalf("seed message: %v", err)
		}
	}
	return id
}

func storedWait(t *testing.T, db *gorm.DB, table, id string) *time.Time {
	t.Helper()
	var row struct {
		At *time.Time `gorm:"column:awaiting_reply_since"`
	}
	if err := db.Raw(`SELECT awaiting_reply_since FROM `+table+` WHERE id = ?`, id).Scan(&row).Error; err != nil {
		t.Fatal(err)
	}
	return row.At
}

func TestTheAwaitingReplyBackfillAgainstPostgres(t *testing.T) {
	db := awaitingReplyFixtures(t)
	now := time.Now().UTC().Truncate(time.Second)
	ago := func(h int) time.Time { return now.Add(-time.Duration(h) * time.Hour) }
	msg := func(sender string, h int) awaitingSeedMessage { return awaitingSeedMessage{sender, ago(h)} }
	kept := ago(30)

	cases := []struct {
		name string
		seed awaitingSeed
		want *time.Time
	}{
		{"an open conversation waits since the first message after the last reply",
			awaitingSeed{status: "ongoing", messages: []awaitingSeedMessage{msg("contact", 10), msg("human", 9), msg("contact", 5), msg("contact", 2)}}, ptrTime(ago(5))},
		{"a campaign blast in between is not an answer",
			awaitingSeed{status: "new", messages: []awaitingSeedMessage{msg("contact", 8), msg("campaign", 6), msg("contact", 3)}}, ptrTime(ago(8))},
		{"an answered conversation waits for nobody",
			awaitingSeed{status: "ongoing", messages: []awaitingSeedMessage{msg("contact", 8), msg("ai", 7)}}, nil},
		{"a finished conversation is left alone",
			awaitingSeed{status: "finished", messages: []awaitingSeedMessage{msg("contact", 8)}}, nil},
		{"a wait already stored is kept",
			awaitingSeed{status: "ongoing", messages: []awaitingSeedMessage{msg("contact", 8)}, stored: &kept}, &kept},
		{"a conversation silent for longer than the window is not scanned",
			awaitingSeed{status: "ongoing", messages: []awaitingSeedMessage{msg("contact", 24*120)}}, nil},
	}

	for _, target := range awaitingReplyTables {
		t.Run(string(target.entryType), func(t *testing.T) {
			ids := make([]string, len(cases))
			for i, c := range cases {
				ids[i] = seedAwaitingConversation(t, db, target.entryType, target.table, c.seed)
			}
			for run := 1; run <= 2; run++ {
				if err := backfillAwaitingReply(db); err != nil {
					t.Fatalf("run %d: %v", run, err)
				}
				for i, c := range cases {
					got := storedWait(t, db, target.table, ids[i])
					switch {
					case c.want == nil && got != nil:
						t.Fatalf("run %d, %s: waiting since %v, want nobody waiting", run, c.name, got.UTC())
					case c.want != nil && (got == nil || !got.Equal(*c.want)):
						t.Fatalf("run %d, %s: waiting since %v, want %v", run, c.name, got, *c.want)
					}
				}
			}
		})
	}
}

func TestTheAwaitingReplyBackfillPlanAgainstPostgres(t *testing.T) {
	db := awaitingReplyFixtures(t)
	for _, name := range []string{"idx_cm_lateral_join", "idx_wce_maxage_lastmsg"} {
		sql, ok := PerformanceIndexSQL(name)
		if !ok {
			t.Fatalf("%s is not declared", name)
		}
		if err := db.Exec(sql).Error; err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	for _, sql := range []string{
		`INSERT INTO whatsapp_campaign_entries (id, campaign_id, lead_id, conversation_status, last_message_at, last_customer_message_at, last_agent_message_at)
			SELECT uuid_in(md5('wce' || i)::cstring), gen_random_uuid(), gen_random_uuid(),
			       CASE WHEN i % 10 < 3 THEN 'finished' WHEN i % 10 < 5 THEN '' ELSE 'ongoing' END,
			       NOW() - ((i % 400) || ' days')::interval,
			       CASE WHEN i % 10 < 5 THEN NULL ELSE NOW() - ((i % 400) || ' days')::interval END,
			       NOW() - ((i % 400) || ' days')::interval - CASE WHEN i % 4 = 0 THEN INTERVAL '-1 hour' ELSE INTERVAL '1 hour' END
			FROM generate_series(1, 100000) AS i`,
		`INSERT INTO conversation_messages (id, entry_id, entry_type, message_type, from_participant, text, sender_kind, created_at, updated_at)
			SELECT md5('m' || i || '-' || k), uuid_in(md5('wce' || i)::cstring), 'whatsapp', 'user_message', 'x', 'oi',
			       CASE WHEN k % 3 = 0 THEN 'human' ELSE 'contact' END,
			       NOW() - ((i % 400) || ' days')::interval - (k || ' hours')::interval, NOW()
			FROM generate_series(1, 100000) AS i, generate_series(1, 4) AS k`,
		`ANALYZE whatsapp_campaign_entries`,
		`ANALYZE conversation_messages`,
	} {
		if err := db.Exec(sql).Error; err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
	}

	official := awaitingReplyTables[0]
	if official.entryType != shared.EntryTypeWhatsApp {
		t.Fatalf("the plan check expects the official table first, got %s", official.entryType)
	}
	var plan []string
	if err := db.Raw("EXPLAIN (ANALYZE, BUFFERS) " + awaitingReplyBackfillSQL(official)).Scan(&plan).Error; err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(plan, "\n")
	t.Logf("backfill plan on 100k official conversations and 400k messages:\n%s", joined)
	if strings.Contains(joined, "Seq Scan on conversation_messages") {
		t.Fatalf("the backfill must reach messages through an index, plan:\n%s", joined)
	}
}

func ptrTime(at time.Time) *time.Time { return &at }
