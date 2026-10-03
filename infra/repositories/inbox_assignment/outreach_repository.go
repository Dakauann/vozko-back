package inbox_assignment_repository

import (
	"gorm.io/gorm"

	"vozko/domain/conversation"
	ia "vozko/domain/inbox_assignment"
	"vozko/infra/database/schema"
)

type outreachRepository struct {
	db *gorm.DB
}

func NewOutreachRepository(db *gorm.DB) ia.EntryOutreachReader {
	return &outreachRepository{db: db}
}

func (r *outreachRepository) LastOutreachBy(entryID, entryType string) (string, error) {
	if entryID == "" || entryType == "" {
		return "", nil
	}

	var senders []string
	if err := r.db.Model(&schema.ConversationMessage{}).
		Where("entry_id = ? AND entry_type = ?", entryID, entryType).
		Where("sender_kind = ?", string(conversation.SenderHuman)).
		Order("created_at DESC").
		Limit(1).
		Pluck("sender_id", &senders).Error; err != nil {
		return "", err
	}
	if len(senders) == 0 {
		return "", nil
	}
	return senders[0], nil
}
