package schema

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type PricingItem struct {
	ID          string    `gorm:"primaryKey;type:uuid"`
	WorkspaceID *string   `gorm:"type:uuid;default:null;uniqueIndex:idx_pricing_item_unique,priority:1"`
	Category    string    `gorm:"type:varchar(50);not null;uniqueIndex:idx_pricing_item_unique,priority:2"`
	Service     string    `gorm:"type:varchar(100);not null;uniqueIndex:idx_pricing_item_unique,priority:3"`
	Metric      string    `gorm:"type:varchar(100);not null;uniqueIndex:idx_pricing_item_unique,priority:4"`
	CostMicros  int64     `gorm:"not null;default:0"`
	PriceMicros int64     `gorm:"not null;default:0"`
	MarkupPct   float64   `gorm:"not null;default:0"`
	Currency    string    `gorm:"type:varchar(3);not null;default:'USD'"`
	CreatedAt   time.Time `gorm:"autoCreateTime"`
	UpdatedAt   time.Time `gorm:"autoUpdateTime"`
}

func (PricingItem) TableName() string { return "pricing_items" }

func (p *PricingItem) BeforeCreate(tx *gorm.DB) error {
	if p.ID == "" {
		p.ID = uuid.New().String()
	}
	return nil
}
