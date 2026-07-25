package schema

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"vozko/infra/crypto/piigorm"
)

type WhatsAppBusinessPhoneNumber struct {
	ID                     string                      `gorm:"primaryKey;type:uuid"`
	Provider               string                      `gorm:"size:20;not null;default:meta;index"`
	MetaPhoneNumberID      string                      `gorm:"uniqueIndex;size:50;not null"`
	WABAId                 string                      `gorm:"size:50;not null;index"`
	OwnerWorkspaceID       string                      `gorm:"type:uuid;index"`
	OwnerAssignedBy        string                      `gorm:"type:uuid"`
	OwnerAssignedAt        *time.Time                  `gorm:"index"`
	BusinessPortfolioID    string                      `gorm:"size:50;index"`
	DisplayPhoneNumber     string                      `gorm:"size:30;not null;index"`
	VerifiedName           string                      `gorm:"size:255"`
	Status                 string                      `gorm:"size:20;not null;default:PENDING;index"`
	QualityRating          string                      `gorm:"size:20;default:UNKNOWN"`
	NameStatus             string                      `gorm:"size:30;default:NONE"`
	CodeVerificationStatus string                      `gorm:"size:20"`
	IsOfficialBusiness     bool                        `gorm:"default:false"`
	BusinessProfile        WhatsAppBusinessProfileJSON `gorm:"type:jsonb"`
	AccessToken            string                      `gorm:"type:text"`
	// Dialog360ChannelID is 360dialog's channel id. Plain index for now; a partial
	// unique index is added in the onboarding phase once the column is populated (a
	// plain uniqueIndex would collide on the empty string every Meta row carries).
	Dialog360ChannelID         string                  `gorm:"size:64;index"`
	Dialog360APIKey            piigorm.EncryptedString `gorm:"column:dialog360_api_key;type:bytea"`
	OnboardingError            string                  `gorm:"type:text"`
	WABAName                   string                  `gorm:"size:255"`
	AccountReviewStatus        string                  `gorm:"size:30"`
	BusinessVerificationStatus string                  `gorm:"size:30"`
	OwnershipType              string                  `gorm:"size:50"`
	MessagingLimitTier         string                  `gorm:"size:30"`
	CallsEnabled               bool                    `gorm:"default:false"`
	CreatedAt                  time.Time               `gorm:"autoCreateTime"`
	UpdatedAt                  time.Time               `gorm:"autoUpdateTime"`
	DeletedAt                  gorm.DeletedAt          `gorm:"index"`
}

func (WhatsAppBusinessPhoneNumber) TableName() string {
	return "whatsapp_business_phone_numbers"
}

func (p *WhatsAppBusinessPhoneNumber) BeforeCreate(tx *gorm.DB) error {
	if p.ID == "" {
		p.ID = uuid.New().String()
	}
	return nil
}

type WhatsAppBusinessProfileJSON struct {
	About             string   `json:"about,omitempty"`
	Address           string   `json:"address,omitempty"`
	Description       string   `json:"description,omitempty"`
	Email             string   `json:"email,omitempty"`
	ProfilePictureURL string   `json:"profilePictureUrl,omitempty"`
	Websites          []string `json:"websites,omitempty"`
	Vertical          string   `json:"vertical,omitempty"`
}

func (p *WhatsAppBusinessProfileJSON) Scan(value interface{}) error {
	if value == nil {
		*p = WhatsAppBusinessProfileJSON{}
		return nil
	}

	bytes, ok := value.([]byte)
	if !ok {
		return errors.New("failed to unmarshal WhatsAppBusinessProfileJSON value")
	}

	if len(bytes) == 0 {
		*p = WhatsAppBusinessProfileJSON{}
		return nil
	}

	return json.Unmarshal(bytes, p)
}

func (p WhatsAppBusinessProfileJSON) Value() (driver.Value, error) {
	return json.Marshal(p)
}
