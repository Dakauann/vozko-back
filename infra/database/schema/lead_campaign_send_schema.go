package schema

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type LeadCampaignSend struct {
	ID              string    `gorm:"primaryKey;type:uuid"`
	LeadID          string    `gorm:"type:uuid;not null;index:idx_lcs_lead_phone,priority:1"`
	BusinessPhoneID string    `gorm:"type:uuid;not null;index:idx_lcs_lead_phone,priority:2"`
	CampaignID      string    `gorm:"type:uuid;not null"`
	SentAt          time.Time `gorm:"not null;index:idx_lcs_lead_phone,priority:3"`
	CreatedAt       time.Time `gorm:"autoCreateTime"`
}

func (LeadCampaignSend) TableName() string {
	return "lead_campaign_sends"
}

func (e *LeadCampaignSend) BeforeCreate(tx *gorm.DB) error {
	if e.ID == "" {
		e.ID = uuid.New().String()
	}
	return nil
}
