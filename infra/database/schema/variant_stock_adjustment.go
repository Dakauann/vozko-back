package schema

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type VariantStockAdjustment struct {
	ID        string         `gorm:"primaryKey;type:uuid"`
	VariantID string         `gorm:"not null;type:uuid;index"`
	Quantity  int            `gorm:"not null"`
	Note      string         `gorm:"type:varchar(255)"`
	CreatedBy string         `gorm:"type:uuid;index"`
	CreatedAt time.Time      `gorm:"autoCreateTime"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime"`
	DeletedAt gorm.DeletedAt `gorm:"index"`
}

func (VariantStockAdjustment) TableName() string {
	return "variant_stock_adjustments"
}

func (vsa *VariantStockAdjustment) BeforeCreate(tx *gorm.DB) error {
	if vsa.ID == "" {
		vsa.ID = uuid.New().String()
	}
	return nil
}
