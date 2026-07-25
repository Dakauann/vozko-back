package schema

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type Call struct {
	ID             string  `gorm:"primaryKey;type:uuid"`
	CallID         string  `gorm:"size:255;not null;uniqueIndex"`
	WorkspaceID    string  `gorm:"type:uuid;not null;index;index:idx_calls_ws_started,priority:1,where:deleted_at IS NULL"`
	Type           string  `gorm:"size:32;not null;index"`
	Direction      string  `gorm:"size:16;not null;index"`
	Source         string  `gorm:"size:32;not null;index"`
	Status         string  `gorm:"size:32;not null;index"`
	AgentID        *string `gorm:"type:uuid;index"`
	ConversationID *string `gorm:"type:uuid;index"`
	LeadID         *string `gorm:"type:uuid;index"`
	PhoneFrom      string  `gorm:"size:64"`
	PhoneTo        string  `gorm:"size:64"`
	TrunkID        *string `gorm:"type:uuid;index"`
	ParentCallID   *string `gorm:"size:255;index"`
	EndReason      *string `gorm:"size:64"`

	StartedAt   time.Time  `gorm:"not null;index:idx_calls_ws_started,priority:2"`
	AnsweredAt  *time.Time `gorm:""`
	EndedAt     *time.Time `gorm:""`
	DurationSec int        `gorm:"not null;default:0"`

	CreatedAt time.Time      `gorm:"autoCreateTime;index"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime"`
	DeletedAt gorm.DeletedAt `gorm:"index"`
}

func (Call) TableName() string {
	return "calls"
}

func (c *Call) BeforeCreate(tx *gorm.DB) error {
	if c.ID == "" {
		c.ID = uuid.New().String()
	}
	return nil
}
