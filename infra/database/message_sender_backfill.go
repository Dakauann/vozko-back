package database

import (
	"context"
	"log"
	"time"

	"gorm.io/gorm"

	"vozko/domain/conversation"
)

const (
	messageSenderBackfillLock  int64 = 7302025092501
	messageSenderBackfillBatch       = 5000
	messageSenderBackfillPause       = 200 * time.Millisecond
)

const legacySenderSQL = `
WITH batch AS (
	SELECT id
	FROM conversation_messages
	WHERE sender_kind = 'unknown'
	ORDER BY id
	LIMIT @limit
	FOR UPDATE SKIP LOCKED
),
classified AS (
	SELECT cm.id,
		cm.from_participant,
		cm.entry_id,
		cm.entry_type,
		a.id AS agent_id,
		CASE
			WHEN cm.message_type IN @customerCalls THEN 'contact'
			WHEN cm.direction = 'INBOUND' OR (cm.direction = '' AND cm.message_type IN @inbound) THEN 'contact'
			WHEN cm.sent_via = 'business_app' THEN 'external'
			WHEN cm.message_type IN @aiTypes THEN 'ai'
			WHEN cm.message_type = 'system' THEN 'system'
			WHEN u.id IS NOT NULL THEN 'human'
			WHEN cm.message_type = 'template' AND cm.entry_type = 'whatsapp' THEN 'campaign'
			WHEN cm.entry_type = 'unofficial_whatsapp' AND cm.from_participant = '' THEN 'campaign'
			WHEN cm.entry_type = 'unofficial_whatsapp' AND cm.message_type IN ('user_message', 'audio') THEN 'external'
			WHEN cm.entry_type IN ('instagram', 'telegram') THEN 'external'
			ELSE 'legacy'
		END AS kind
	FROM conversation_messages cm
	JOIN batch b ON b.id = cm.id
	LEFT JOIN users u ON u.id::text = cm.from_participant
	LEFT JOIN agents a ON a.id::text = cm.from_participant
)
UPDATE conversation_messages m
SET sender_kind = c.kind,
	sender_id = CASE c.kind
		WHEN 'contact' THEN c.from_participant
		WHEN 'human' THEN c.from_participant
		WHEN 'ai' THEN COALESCE('ai:' || c.agent_id::text, '')
		WHEN 'campaign' THEN COALESCE(
			CASE WHEN c.entry_type = 'whatsapp'
				THEN (SELECT 'campaign:' || wce.campaign_id::text FROM whatsapp_campaign_entries wce WHERE wce.id = c.entry_id)
				ELSE (SELECT 'campaign:' || uwc.campaign_id FROM unofficial_whatsapp_conversations uwc WHERE uwc.id = c.entry_id AND uwc.campaign_id <> '')
			END, '')
		ELSE ''
	END
FROM classified c
WHERE m.id = c.id`

func BackfillMessageSendersBatch(tx *gorm.DB, limit int) (int64, error) {
	result := tx.Exec(legacySenderSQL, map[string]interface{}{
		"limit":   limit,
		"inbound": conversation.InboundMessageTypeStrings(),
		"customerCalls": []string{
			string(conversation.MessageTypeCallReceived),
			string(conversation.MessageTypeCallMissed),
			string(conversation.MessageTypeCallAnswered),
			string(conversation.MessageTypeCallEnded),
			string(conversation.MessageTypeCallPermissionGranted),
			string(conversation.MessageTypeCallPermissionRejected),
		},
		"aiTypes": []string{
			string(conversation.MessageTypeAIResponse),
			string(conversation.MessageTypeToolCall),
			string(conversation.MessageTypeToolResult),
		},
	})
	return result.RowsAffected, result.Error
}

func RunMessageSenderBackfill(ctx context.Context, db *gorm.DB) {
	var total int64
	for ctx.Err() == nil {
		var attributed int64
		acquired := false
		err := db.Transaction(func(tx *gorm.DB) error {
			if err := tx.Raw("SELECT pg_try_advisory_xact_lock(?)", messageSenderBackfillLock).Scan(&acquired).Error; err != nil || !acquired {
				return err
			}
			n, err := BackfillMessageSendersBatch(tx, messageSenderBackfillBatch)
			attributed = n
			return err
		})
		if err != nil {
			log.Printf("[message-senders] backfill stopped after %d message(s): %v", total, err)
			return
		}
		if !acquired || attributed == 0 {
			if total > 0 {
				log.Printf("[message-senders] backfill finished: %d message(s) attributed", total)
			}
			return
		}
		total += attributed
		time.Sleep(messageSenderBackfillPause)
	}
}
