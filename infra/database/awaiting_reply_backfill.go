package database

import (
	"fmt"

	"gorm.io/gorm"

	"vozko/domain/conversation"
	"vozko/domain/shared"
	"vozko/infra/database/sqlexpr"
)

const (
	awaitingReplyBackfillBatch  = 20000
	awaitingReplyBackfillWindow = "90 days"
)

type awaitingReplyTable struct {
	entryType shared.EntryType
	table     string
}

var awaitingReplyTables = []awaitingReplyTable{
	{shared.EntryTypeWhatsApp, "whatsapp_campaign_entries"},
	{shared.EntryTypeInstagram, "instagram_conversations"},
	{shared.EntryTypeFacebook, "facebook_conversations"},
	{shared.EntryTypeTelegram, "telegram_conversations"},
	{shared.EntryTypeWebchat, "webchat_conversations"},
	{shared.EntryTypeMercadoLivre, "mercadolivre_conversations"},
	{shared.EntryTypeTikTok, "tiktok_conversations"},
	{shared.EntryTypeEmail, "email_conversations"},
	{shared.EntryTypeRCS, "rcs_conversations"},
	{shared.EntryTypeSMS, "sms_conversations"},
	{shared.EntryTypeUnofficialWhatsApp, "unofficial_whatsapp_conversations"},
}

func AwaitingReplyBackfillTables() map[shared.EntryType]string {
	out := make(map[shared.EntryType]string, len(awaitingReplyTables))
	for _, t := range awaitingReplyTables {
		out[t.entryType] = t.table
	}
	return out
}

func backfillAwaitingReply(tx *gorm.DB) error {
	if !tx.Migrator().HasTable("conversation_messages") {
		return nil
	}
	for _, target := range awaitingReplyTables {
		if !tx.Migrator().HasColumn(target.table, sqlexpr.AwaitingReplySinceColumn) {
			continue
		}
		if err := tx.Exec(awaitingReplyBackfillSQL(target)).Error; err != nil {
			return fmt.Errorf("%s: %w", target.table, err)
		}
	}
	return nil
}

func awaitingReplyBackfillSQL(target awaitingReplyTable) string {
	open := sqlexpr.LiteralList([]string{
		string(conversation.ConversationStatusNew),
		string(conversation.ConversationStatusOngoing),
	})
	return fmt.Sprintf(`
		WITH candidates AS (
			SELECT c.id FROM %[1]s c
			WHERE c.deleted_at IS NULL
			  AND c.conversation_status IN (%[2]s)
			  AND c.%[3]s IS NULL
			  AND c.last_message_at > NOW() - INTERVAL '%[4]s'
			  AND c.last_customer_message_at > NOW() - INTERVAL '%[4]s'
			  AND (c.last_agent_message_at IS NULL OR c.last_customer_message_at > c.last_agent_message_at)
			LIMIT %[5]d
		),
		computed AS MATERIALIZED (
			SELECT candidates.id, %[6]s AS since FROM candidates
		)
		UPDATE %[1]s AS u SET %[3]s = computed.since
		FROM computed
		WHERE u.id = computed.id AND computed.since IS NOT NULL`,
		target.table, open, sqlexpr.AwaitingReplySinceColumn, awaitingReplyBackfillWindow, awaitingReplyBackfillBatch,
		sqlexpr.FirstUnansweredCustomerMessage("candidates.id", sqlexpr.Literal(string(target.entryType))))
}
