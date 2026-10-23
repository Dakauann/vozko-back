package schema

import (
	"time"

	"github.com/lib/pq"
)

type ConversationNote struct {
	ID          string         `gorm:"primaryKey;type:uuid"`
	WorkspaceID string         `gorm:"type:uuid;not null"`
	EntryID     string         `gorm:"type:uuid;not null;index:idx_conversation_notes_entry,priority:1,where:deleted_at IS NULL"`
	EntryType   string         `gorm:"size:32;not null;index:idx_conversation_notes_entry,priority:2"`
	AuthorID    string         `gorm:"type:uuid;not null"`
	Body        string         `gorm:"type:text;not null"`
	Mentions    pq.StringArray `gorm:"type:uuid[];not null;default:'{}'"`
	ImageIDs    pq.StringArray `gorm:"column:image_ids;type:uuid[];not null;default:'{}'"`
	CreatedAt   time.Time      `gorm:"type:timestamptz;not null;index:idx_conversation_notes_entry,priority:3,sort:desc"`
	DeletedAt   *time.Time     `gorm:"type:timestamptz"`
}

func (ConversationNote) TableName() string { return "conversation_notes" }
