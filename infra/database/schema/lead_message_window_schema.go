package schema

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type LeadMessageWindow struct {
	ID              string    `gorm:"primaryKey;type:uuid"`
	LeadID          string    `gorm:"type:uuid;not null;index;uniqueIndex:uni_lead_business_phone,priority:1"`
	BusinessPhoneID string    `gorm:"type:uuid;not null;index;uniqueIndex:uni_lead_business_phone,priority:2"`
	LastMessageAt   time.Time `gorm:"not null"`
	CreatedAt       time.Time `gorm:"autoCreateTime"`
	UpdatedAt       time.Time `gorm:"autoUpdateTime"`

	Lead          Lead                        `gorm:"foreignKey:LeadID;references:ID"`
	BusinessPhone WhatsAppBusinessPhoneNumber `gorm:"foreignKey:BusinessPhoneID;references:ID"`
}

func (LeadMessageWindow) TableName() string {
	return "lead_message_windows"
}

func (w *LeadMessageWindow) BeforeCreate(tx *gorm.DB) error {
	if w.ID == "" {
		w.ID = uuid.New().String()
	}
	return nil
}
