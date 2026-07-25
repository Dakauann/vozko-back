package schema

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"vozko/domain/message_shortcut"
)

type MessageShortcut struct {
	ID          string                   `gorm:"primaryKey;type:uuid"`
	WorkspaceID string                   `gorm:"type:uuid;not null;index:idx_msg_shortcut_ws;uniqueIndex:idx_msg_shortcut_ws_shortcut,priority:1"`
	Shortcut    string                   `gorm:"size:50;not null;uniqueIndex:idx_msg_shortcut_ws_shortcut,priority:2"`
	Name        string                   `gorm:"size:100;not null"`
	MessageType string                   `gorm:"size:20;not null"`
	Content     message_shortcut.Content `gorm:"type:jsonb;not null;default:'{}'"`
	CreatedAt   time.Time                `gorm:"autoCreateTime"`
	UpdatedAt   time.Time                `gorm:"autoUpdateTime"`
	DeletedAt   gorm.DeletedAt           `gorm:"index"`
}

func (MessageShortcut) TableName() string {
	return "message_shortcuts"
}

func (m *MessageShortcut) BeforeCreate(tx *gorm.DB) error {
	if m.ID == "" {
		m.ID = uuid.New().String()
	}
	return nil
}
