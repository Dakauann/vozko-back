package schema

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type PlanPricingItem struct {
	ID               string    `gorm:"primaryKey;type:uuid"`
	PlanDefinitionID string    `gorm:"type:uuid;not null;uniqueIndex:idx_plan_pricing_item_unique,priority:1"`
	Category         string    `gorm:"type:varchar(50);not null;uniqueIndex:idx_plan_pricing_item_unique,priority:2"`
	Service          string    `gorm:"type:varchar(100);not null;uniqueIndex:idx_plan_pricing_item_unique,priority:3"`
	Metric           string    `gorm:"type:varchar(100);not null;uniqueIndex:idx_plan_pricing_item_unique,priority:4"`
	CostMicros       int64     `gorm:"not null;default:0"`
	PriceMicros      int64     `gorm:"not null;default:0"`
	MarkupPct        float64   `gorm:"not null;default:0"`
	Currency         string    `gorm:"type:varchar(3);not null;default:'USD'"`
	CreatedAt        time.Time `gorm:"autoCreateTime"`
	UpdatedAt        time.Time `gorm:"autoUpdateTime"`

	PlanDefinition WorkspacePlanDefinition `gorm:"foreignKey:PlanDefinitionID;constraint:OnDelete:CASCADE"`
}

func (PlanPricingItem) TableName() string { return "plan_pricing_items" }

func (p *PlanPricingItem) BeforeCreate(tx *gorm.DB) error {
	if p.ID == "" {
		p.ID = uuid.New().String()
	}
	return nil
}
