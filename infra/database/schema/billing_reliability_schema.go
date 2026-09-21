package schema

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type ProcessedWebhookEvent struct {
	ID          string    `gorm:"primaryKey;type:uuid"`
	Provider    string    `gorm:"type:varchar(32);not null;uniqueIndex:ux_processed_webhook_provider_event,priority:1"`
	EventID     string    `gorm:"type:varchar(255);not null;uniqueIndex:ux_processed_webhook_provider_event,priority:2"`
	ProcessedAt time.Time `gorm:"autoCreateTime;index"`
}

func (ProcessedWebhookEvent) TableName() string { return "processed_webhook_events" }

func (e *ProcessedWebhookEvent) BeforeCreate(tx *gorm.DB) error {
	if e.ID == "" {
		e.ID = uuid.New().String()
	}
	return nil
}
