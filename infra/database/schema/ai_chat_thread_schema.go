package schema

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// AIChatThread is a user-owned conversation in the in-app AI chat. Threads are
// scoped to a workspace (for billing/access control) and to the user who created
// them. The list index supports "most recent threads for this user/workspace".
type AIChatThread struct {
	ID            string         `gorm:"primaryKey;type:text"`
	WorkspaceID   string         `gorm:"type:uuid;not null;index:idx_chat_thread_ws_user,priority:1"`
	UserID        string         `gorm:"type:uuid;not null;index:idx_chat_thread_ws_user,priority:2"`
	Title         string         `gorm:"type:text"`
	Model         string         `gorm:"type:text"`
	LastMessageAt *time.Time     `gorm:"type:timestamptz;index:idx_chat_thread_ws_user,priority:3,sort:desc"`
	CreatedAt     time.Time      `gorm:"autoCreateTime"`
	UpdatedAt     time.Time      `gorm:"autoUpdateTime"`
	DeletedAt     gorm.DeletedAt `gorm:"index"`
}

func (AIChatThread) TableName() string {
	return "ai_chat_threads"
}

func (t *AIChatThread) BeforeCreate(tx *gorm.DB) error {
	if t.ID == "" {
		t.ID = uuid.New().String()
	}
	return nil
}
