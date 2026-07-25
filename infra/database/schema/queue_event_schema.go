package schema

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type QueueEvent struct {
	ID          string    `gorm:"primaryKey;type:text"`
	WorkspaceID string    `gorm:"type:uuid;not null;index:idx_queue_ev_ws_time,priority:1"`
	TransferID  string    `gorm:"type:text;default:'';index"`
	CallID      string    `gorm:"type:text;default:'';index"`
	TargetKind  string    `gorm:"type:varchar(32);default:''"`
	TargetID    string    `gorm:"type:text;default:''"`
	Type        string    `gorm:"type:varchar(32);not null;index:idx_queue_ev_ws_time,priority:3"`
	Position    int       `gorm:"not null;default:0"`
	WaitedMS    int64     `gorm:"not null;default:0"`
	OccurredAt  time.Time `gorm:"not null;index:idx_queue_ev_ws_time,priority:2"`
	CreatedAt   time.Time `gorm:"autoCreateTime"`
}

func (QueueEvent) TableName() string {
	return "queue_events"
}

func (e *QueueEvent) BeforeCreate(tx *gorm.DB) error {
	if e.ID == "" {
		e.ID = uuid.New().String()
	}
	return nil
}
