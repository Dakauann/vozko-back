package schema

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type AgentPresenceInterval struct {
	ID          string     `gorm:"primaryKey;type:text"`
	WorkspaceID string     `gorm:"type:uuid;not null;index:idx_presence_ws_user,priority:1;index:idx_presence_open,priority:1"`
	UserID      string     `gorm:"type:uuid;not null;index:idx_presence_ws_user,priority:2;index:idx_presence_open,priority:2"`
	State       string     `gorm:"type:varchar(16);not null"`
	Source      string     `gorm:"type:varchar(32);not null;default:''"`
	StartedAt   time.Time  `gorm:"not null;index:idx_presence_ws_user,priority:3"`
	EndedAt     *time.Time `gorm:"index:idx_presence_open,priority:3,where:ended_at IS NULL"`
	CreatedAt   time.Time  `gorm:"autoCreateTime"`
}

func (AgentPresenceInterval) TableName() string {
	return "agent_presence_intervals"
}

func (i *AgentPresenceInterval) BeforeCreate(tx *gorm.DB) error {
	if i.ID == "" {
		i.ID = uuid.New().String()
	}
	return nil
}
