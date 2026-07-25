package schema

import (
	"time"

	"gorm.io/gorm"
)

type SystemConfig struct {
	ID                     string    `gorm:"primaryKey;type:varchar(36);default:'default'"`
	BaseSystemPrompt       string    `gorm:"type:text"`
	MaxConcurrentCalls     int       `gorm:"column:max_concurrent_calls;type:int;default:30"`
	WorkTimeEnabled        bool      `gorm:"type:bool;default:false"`
	WorkTimeStart          string    `gorm:"type:varchar(5);default:'08:00'"`
	WorkTimeEnd            string    `gorm:"type:varchar(5);default:'20:00'"`
	AffiliateCommissionPct float64   `gorm:"type:decimal(5,4);default:0.05"`
	UpdatedBy              string    `gorm:"type:uuid"`
	UpdatedAt              time.Time `gorm:"autoUpdateTime"`
	CreatedAt              time.Time `gorm:"autoCreateTime"`
}

func (SystemConfig) TableName() string {
	return "system_config"
}

func (c *SystemConfig) BeforeCreate(tx *gorm.DB) error {
	if c.ID == "" {
		c.ID = "default"
	}
	return nil
}
