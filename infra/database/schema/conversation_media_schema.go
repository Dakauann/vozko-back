package schema

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type ConversationMedia struct {
	ID               string         `gorm:"primaryKey;type:uuid"`
	EntryID          string         `gorm:"type:uuid;not null;index;index:idx_cmedia_entry,priority:1"`
	EntryType        string         `gorm:"type:varchar(20);not null;index:idx_cmedia_entry,priority:2"`
	Type             string         `gorm:"type:varchar(30);not null"`
	MimeType         string         `gorm:"type:varchar(100)"`
	URL              string         `gorm:"type:text;not null"`
	OriginalFilename string         `gorm:"type:varchar(255)"`
	SizeBytes        int64          `gorm:"type:bigint"`
	DurationSeconds  *int           `gorm:"type:integer"`
	WhatsAppMediaID  string         `gorm:"type:varchar(255);index"`
	CreatedAt        time.Time      `gorm:"autoCreateTime"`
	DeletedAt        gorm.DeletedAt `gorm:"index"`
}

func (ConversationMedia) TableName() string {
	return "conversation_media"
}

func (m *ConversationMedia) BeforeCreate(tx *gorm.DB) error {
	if m.ID == "" {
		m.ID = uuid.New().String()
	}
	return nil
}
