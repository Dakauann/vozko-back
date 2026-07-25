package schema

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ProcessedWebhookEvent is the durable webhook idempotency guard. The composite unique index on
// (provider, event_id) makes a second insert for the same event a no-op under
// ON CONFLICT DO NOTHING, which is how a redelivery is rejected even after Redis has lost its
// short-lived dedup key. This is the money-safe backstop for credit/activation side effects.
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
