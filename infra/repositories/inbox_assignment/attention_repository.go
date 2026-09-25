package inbox_assignment_repository

import (
	"time"

	"gorm.io/gorm"

	"vozko/domain/conversation"
	ia "vozko/domain/inbox_assignment"
	"vozko/infra/database/schema"
)

type attentionRepository struct {
	db *gorm.DB
}

func NewAttentionRepository(db *gorm.DB) ia.EntryAttentionReader {
	return &attentionRepository{db: db}
}

func (r *attentionRepository) AttendedSince(entryID, entryType, assignedUserID string, since time.Time) (bool, error) {
	if entryID == "" || entryType == "" {
		return false, nil
	}

	q := r.db.Model(&schema.ConversationMessage{}).
		Where("entry_id = ? AND entry_type = ?", entryID, entryType).
		Where(
			r.db.Where("read = ? AND read_by = ? AND read_at >= ?", true, assignedUserID, since).
				Or("created_at >= ? AND sender_id = ?", since, conversation.SentByPerson(assignedUserID).ID()),
		)

	var count int64
	if err := q.Limit(1).Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}
