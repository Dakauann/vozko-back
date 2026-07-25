package schema

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type WorkflowWebhookSchema struct {
	ID          string    `gorm:"primaryKey;type:uuid"`
	WorkflowID  string    `gorm:"type:uuid;not null;uniqueIndex"`
	WorkspaceID string    `gorm:"type:uuid;not null;index"`
	Token       string    `gorm:"not null;size:80;uniqueIndex"`
	AuthMode    string    `gorm:"not null;size:20;default:header_token"`
	Secret      string    `gorm:"size:255"`
	HeaderName  string    `gorm:"size:100"`
	Method      string    `gorm:"not null;size:10;default:POST"`
	Active      bool      `gorm:"not null;default:true"`
	CreatedAt   time.Time `gorm:"autoCreateTime"`
	UpdatedAt   time.Time `gorm:"autoUpdateTime"`
}

func (WorkflowWebhookSchema) TableName() string { return "workflow_webhooks" }

func (w *WorkflowWebhookSchema) BeforeCreate(tx *gorm.DB) error {
	if w.ID == "" {
		w.ID = uuid.New().String()
	}
	return nil
}
