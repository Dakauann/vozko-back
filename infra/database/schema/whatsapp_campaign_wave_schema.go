package schema

import (
	"time"

	"gorm.io/datatypes"
)

type WhatsAppCampaignWavePolicy struct {
	CampaignID string         `gorm:"primaryKey;type:uuid"`
	Policy     datatypes.JSON `gorm:"type:jsonb;not null"`
	CreatedAt  time.Time      `gorm:"autoCreateTime"`
	UpdatedAt  time.Time      `gorm:"autoUpdateTime"`
}

func (WhatsAppCampaignWavePolicy) TableName() string { return "whatsapp_campaign_wave_policies" }

type WhatsAppCampaignWaveState struct {
	CampaignID       string         `gorm:"primaryKey;type:uuid"`
	Hold             datatypes.JSON `gorm:"type:jsonb"`
	Waiting          datatypes.JSON `gorm:"type:jsonb"`
	ResumedAfterHold bool           `gorm:"not null;default:false"`
	UnreadSince      *time.Time     `gorm:"type:timestamptz"`
	DueAt            *time.Time     `gorm:"type:timestamptz;index:idx_wa_campaign_wave_state_due"`
	UpdatedAt        time.Time      `gorm:"autoUpdateTime"`
}

func (WhatsAppCampaignWaveState) TableName() string { return "whatsapp_campaign_wave_states" }

type WhatsAppCampaignWave struct {
	CampaignID   string         `gorm:"primaryKey;type:uuid"`
	Number       int            `gorm:"primaryKey"`
	Size         int            `gorm:"not null"`
	State        string         `gorm:"size:16;not null"`
	ReleasedAt   time.Time      `gorm:"type:timestamptz;not null"`
	DrainedAt    *time.Time     `gorm:"type:timestamptz"`
	ObserveUntil *time.Time     `gorm:"type:timestamptz"`
	DecidedAt    *time.Time     `gorm:"type:timestamptz"`
	Outcome      datatypes.JSON `gorm:"type:jsonb"`
	Decision     datatypes.JSON `gorm:"type:jsonb"`
}

func (WhatsAppCampaignWave) TableName() string { return "whatsapp_campaign_waves" }
