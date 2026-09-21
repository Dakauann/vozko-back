package schema

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type ScheduledMessage struct {
	ID          string `gorm:"primaryKey;type:uuid"`
	WorkspaceID string `gorm:"type:uuid;not null;index:idx_sched_msg_ws"`
	EntryID     string `gorm:"type:uuid;not null;index:idx_sched_msg_entry,priority:1"`
	EntryType   string `gorm:"size:32;not null;index:idx_sched_msg_entry,priority:2"`

	CreatedByUserID  string  `gorm:"type:uuid;not null;index"`
	Text             string  `gorm:"type:text"`
	MediaID          *string `gorm:"type:uuid"`
	MediaType        *string `gorm:"size:20"`
	ReplyToMessageID *string `gorm:"type:uuid"`
	Signed           bool    `gorm:"not null;default:false"`

	ScheduledAt               time.Time `gorm:"not null;index"`
	WindowExpiresAtAtCreation *time.Time

	Status        string  `gorm:"size:20;not null;default:'pending';index"`
	FailureReason *string `gorm:"size:40"`
	FailureDetail string  `gorm:"type:text"`

	ClaimedAt     *time.Time
	SentAt        *time.Time
	SentMessageID *string `gorm:"type:uuid"`

	IdempotencyKey *string `gorm:"size:128"`

	CreatedAt time.Time      `gorm:"autoCreateTime"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime"`
	DeletedAt gorm.DeletedAt `gorm:"index"`
}

func (ScheduledMessage) TableName() string {
	return "scheduled_messages"
}

func (m *ScheduledMessage) BeforeCreate(tx *gorm.DB) error {
	if m.ID == "" {
		m.ID = uuid.New().String()
	}
	return nil
}
