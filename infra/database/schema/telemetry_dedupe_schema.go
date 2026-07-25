package schema

import (
	"time"

	"gorm.io/gorm"
)

// TelemetryDedupe records processed crm_telemetry envelope IDs for idempotent consume.
type TelemetryDedupe struct {
	ID        string    `gorm:"primaryKey;type:text"`
	Kind      string    `gorm:"type:varchar(40);not null;index"`
	CreatedAt time.Time `gorm:"autoCreateTime;index"`
}

func (TelemetryDedupe) TableName() string {
	return "telemetry_dedupe"
}

func (t *TelemetryDedupe) BeforeCreate(tx *gorm.DB) error {
	return nil
}
