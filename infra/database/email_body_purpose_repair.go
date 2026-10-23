package database

import (
	"gorm.io/gorm"

	"vozko/domain/conversation"
	em "vozko/domain/emailinbox"
	"vozko/domain/shared"
)

func markEmailBodies(tx *gorm.DB) error {
	if !tx.Migrator().HasTable("conversation_messages") || !tx.Migrator().HasColumn("conversation_media", "purpose") {
		return nil
	}
	return tx.Exec(`
		UPDATE conversation_media m SET purpose = ?
		FROM conversation_messages msg
		WHERE m.purpose <> ?
		  AND m.entry_type = ?
		  AND msg.entry_type = m.entry_type
		  AND msg.entry_id = m.entry_id
		  AND msg.metadata->>'`+em.MetadataHTMLMediaID+`' = m.id::text`,
		string(conversation.MediaPurposeEmailBody), string(conversation.MediaPurposeEmailBody), string(shared.EntryTypeEmail)).Error
}
