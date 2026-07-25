package schema

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type PricingAuditLog struct {
	ID             string    `gorm:"primaryKey;type:uuid"`
	WorkspaceID    *string   `gorm:"type:uuid;default:null;index"`
	Category       string    `gorm:"type:varchar(50);not null"`
	Service        string    `gorm:"type:varchar(100);not null"`
	Metric         string    `gorm:"type:varchar(100);not null"`
	OldPriceMicros int64     `gorm:"not null"`
	NewPriceMicros int64     `gorm:"not null"`
	Currency       string    `gorm:"type:varchar(3);not null"`
	ChangedBy      string    `gorm:"type:uuid;not null"`
	ChangedAt      time.Time `gorm:"autoCreateTime"`
}

func (PricingAuditLog) TableName() string { return "pricing_audit_log" }

func (p *PricingAuditLog) BeforeCreate(tx *gorm.DB) error {
	if p.ID == "" {
		p.ID = uuid.New().String()
	}
	return nil
}
