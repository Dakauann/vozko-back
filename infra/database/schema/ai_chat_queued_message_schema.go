package schema

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type AIChatQueuedMessage struct {
	ID          string     `gorm:"primaryKey;type:text"`
	ThreadID    string     `gorm:"type:uuid;not null;uniqueIndex:idx_chat_queue_client,priority:1;index:idx_chat_queue_pending,priority:1"`
	ClientID    string     `gorm:"type:varchar(64);not null;uniqueIndex:idx_chat_queue_client,priority:2"`
	Content     string     `gorm:"type:text"`
	Attachments []byte     `gorm:"type:jsonb"`
	MessageID   *string    `gorm:"type:text"`
	CreatedAt   time.Time  `gorm:"autoCreateTime;index:idx_chat_queue_pending,priority:2"`
	DeliveredAt *time.Time `gorm:"index"`
}

func (AIChatQueuedMessage) TableName() string {
	return "ai_chat_queued_messages"
}

func (m *AIChatQueuedMessage) BeforeCreate(tx *gorm.DB) error {
	if m.ID == "" {
		m.ID = uuid.New().String()
	}
	return nil
}
