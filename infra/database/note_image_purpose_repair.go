package database

import (
	"gorm.io/gorm"

	"vozko/domain/conversation"
)

func markNoteImages(tx *gorm.DB) error {
	if !tx.Migrator().HasTable("conversation_notes") || !tx.Migrator().HasColumn("conversation_media", "purpose") {
		return nil
	}
	return tx.Exec(`
		UPDATE conversation_media SET purpose = ?
		WHERE purpose <> ?
		  AND id IN (SELECT unnest(image_ids) FROM conversation_notes WHERE cardinality(image_ids) > 0)`,
		string(conversation.MediaPurposeNote), string(conversation.MediaPurposeNote)).Error
}
