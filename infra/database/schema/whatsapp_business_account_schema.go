package schema

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type WhatsAppBusinessAccount struct {
	ID                         string         `gorm:"primaryKey;type:uuid"`
	Provider                   string         `gorm:"size:20;not null;default:meta;index"`
	MetaWABAId                 string         `gorm:"uniqueIndex;size:50;not null"`
	Name                       string         `gorm:"size:255"`
	BusinessPortfolioID        string         `gorm:"size:50;index"`
	Dialog360ClientID          string         `gorm:"size:64;index"`
	AccessToken                string         `gorm:"type:text"`
	AccountReviewStatus        string         `gorm:"size:30"`
	BusinessVerificationStatus string         `gorm:"size:30"`
	OwnershipType              string         `gorm:"size:50"`
	MessagingLimitTier         string         `gorm:"size:30"`
	Currency                   string         `gorm:"size:10"`
	TimezoneId                 string         `gorm:"size:10"`
	MessageTemplateNamespace   string         `gorm:"size:255"`
	Status                     string         `gorm:"size:20"`
	Country                    string         `gorm:"size:10"`
	CreatedAt                  time.Time      `gorm:"autoCreateTime"`
	UpdatedAt                  time.Time      `gorm:"autoUpdateTime"`
	DeletedAt                  gorm.DeletedAt `gorm:"index"`
}

func (WhatsAppBusinessAccount) TableName() string {
	return "whatsapp_business_accounts"
}

func (w *WhatsAppBusinessAccount) BeforeCreate(tx *gorm.DB) error {
	if w.ID == "" {
		w.ID = uuid.New().String()
	}
	return nil
}
