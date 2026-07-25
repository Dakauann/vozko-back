package schema

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type WorkspaceSubscription struct {
	ID                 string     `gorm:"primaryKey;type:uuid"`
	WorkspaceID        string     `gorm:"type:uuid;not null;index"`
	PlanDefinitionID   string     `gorm:"type:uuid;not null;index"`
	PlanName           string     `gorm:"type:varchar(255);not null"`
	MaxCallChannels    int        `gorm:"not null;default:0"`
	BillingCycle       string     `gorm:"type:varchar(20);not null;default:'monthly'"`
	Status             string     `gorm:"type:varchar(20);not null;index"`
	CurrentPeriodStart time.Time  `gorm:"not null;index"`
	CurrentPeriodEnd   time.Time  `gorm:"not null;index"`
	CancelledAt        *time.Time `gorm:"index"`
	CreatedAt          time.Time  `gorm:"autoCreateTime"`
	UpdatedAt          time.Time  `gorm:"autoUpdateTime"`
}

func (WorkspaceSubscription) TableName() string { return "workspace_subscriptions" }

func (s *WorkspaceSubscription) BeforeCreate(tx *gorm.DB) error {
	if s.ID == "" {
		s.ID = uuid.New().String()
	}
	return nil
}
