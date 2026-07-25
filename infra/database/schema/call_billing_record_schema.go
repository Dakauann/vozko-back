package schema

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type CallBillingRecord struct {
	ID          string  `gorm:"primaryKey;type:uuid"`
	CallID      string  `gorm:"size:255;not null;uniqueIndex"`
	WorkspaceID string  `gorm:"type:uuid;not null;index;index:idx_cbr_ws_callstart_status,priority:1,where:deleted_at IS NULL;index:idx_cbr_ws_agent_callstart,priority:1,where:deleted_at IS NULL AND agent_id IS NOT NULL"`
	AgentID     *string `gorm:"type:uuid;index:idx_cbr_ws_agent_callstart,priority:2"`
	LeadID      *string `gorm:"type:uuid"`
	CallSource  string  `gorm:"size:20;not null"`
	Status      string  `gorm:"size:20;not null;index;index:idx_cbr_ws_callstart_status,priority:3;index:idx_cbr_callstart_status,priority:2"`

	CallStart   time.Time `gorm:"not null;index:idx_cbr_ws_callstart_status,priority:2;index:idx_cbr_ws_agent_callstart,priority:3;index:idx_cbr_callstart_status,priority:1,where:deleted_at IS NULL"`
	CallEnd     time.Time `gorm:"not null"`
	DurationSec int       `gorm:"not null;default:0"`

	TelephonyRevenueMicros int64 `gorm:"not null;default:0"`
	TotalRevenueMicros     int64 `gorm:"not null;default:0"`

	TelephonyCostMicros int64 `gorm:"not null;default:0"`
	TotalCostMicros     int64 `gorm:"not null;default:0"`

	TelephonyProfitMicros int64 `gorm:"not null;default:0"`
	TotalProfitMicros     int64 `gorm:"not null;default:0"`

	TransactionID *string `gorm:"type:uuid"`
	RecordingURL  *string `gorm:"size:1000"`
	CallRecordID  *string `gorm:"type:uuid;index"`

	CreatedAt time.Time      `gorm:"autoCreateTime;index"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime"`
	DeletedAt gorm.DeletedAt `gorm:"index"`
}

func (CallBillingRecord) TableName() string {
	return "call_billing_records"
}

func (r *CallBillingRecord) BeforeCreate(tx *gorm.DB) error {
	if r.ID == "" {
		r.ID = uuid.New().String()
	}
	return nil
}
