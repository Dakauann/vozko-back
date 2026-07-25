package schema

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type ConversationEvent struct {
	ID            string    `gorm:"primaryKey;type:text"`
	WorkspaceID   string    `gorm:"type:uuid;not null;index:idx_conv_event_ws"`
	EntryID       string    `gorm:"type:uuid;not null;index:idx_conv_event_entry,priority:1"`
	EntryType     string    `gorm:"type:varchar(20);not null;index:idx_conv_event_entry,priority:2"`
	EventType     string    `gorm:"type:varchar(40);not null;index:idx_conv_event_type"`
	ActorID       string    `gorm:"type:text;not null;index:idx_conv_event_actor"`
	ActorKind     string    `gorm:"type:varchar(16);not null;default:'human';index:idx_conv_event_actor_kind"`
	Channel       string    `gorm:"type:varchar(20);default:''"`
	CorrelationID string    `gorm:"type:text;default:''"`
	Details       string    `gorm:"type:text"`
	CreatedAt     time.Time `gorm:"autoCreateTime;index:idx_conv_event_created"`
}

func (ConversationEvent) TableName() string {
	return "conversation_events"
}

func (e *ConversationEvent) BeforeCreate(tx *gorm.DB) error {
	if e.ID == "" {
		e.ID = uuid.New().String()
	}
	if e.ActorKind == "" {
		e.ActorKind = "human"
	}
	return nil
}
