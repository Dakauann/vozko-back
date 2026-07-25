package schema

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type BusinessMetric struct {
	ID         string         `gorm:"primaryKey;type:uuid"`
	EventType  string         `gorm:"not null;size:64;index"`
	EntityID   string         `gorm:"not null;size:256;index"`
	EntityType string         `gorm:"not null;size:64;index"`
	UserID     *string        `gorm:"type:uuid;index"`
	Metadata   string         `gorm:"type:jsonb"`
	OccurredAt time.Time      `gorm:"not null;index"`
	CreatedAt  time.Time      `gorm:"autoCreateTime"`
	DeletedAt  gorm.DeletedAt `gorm:"index"`
}

func (BusinessMetric) TableName() string {
	return "business_metrics"
}

func (m *BusinessMetric) BeforeCreate(tx *gorm.DB) error {
	if m.ID == "" {
		m.ID = uuid.New().String()
	}
	if m.OccurredAt.IsZero() {
		m.OccurredAt = time.Now()
	}
	return nil
}
