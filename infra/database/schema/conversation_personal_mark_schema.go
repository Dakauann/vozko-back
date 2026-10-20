package schema

import "time"

type ConversationPersonalMark struct {
	UserID         string     `gorm:"primaryKey;type:uuid"`
	EntryType      string     `gorm:"primaryKey;size:32;index:idx_personal_marks_entry,priority:2"`
	EntryID        string     `gorm:"primaryKey;type:uuid;index:idx_personal_marks_entry,priority:1"`
	PinnedAt       *time.Time `gorm:"type:timestamptz"`
	UnreadMarkedAt *time.Time `gorm:"type:timestamptz"`
	UpdatedAt      time.Time  `gorm:"type:timestamptz;not null;autoUpdateTime"`
}

func (ConversationPersonalMark) TableName() string { return "conversation_personal_marks" }
