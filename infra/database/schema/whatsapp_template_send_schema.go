package schema

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type WhatsAppTemplateSend struct {
	ID             string `gorm:"primaryKey;type:uuid"`
	WorkspaceID    string `gorm:"type:uuid;not null;index:idx_wats_ws"`
	UserID         string `gorm:"type:uuid;index"`
	IdempotencyKey string `gorm:"size:191;not null"`

	BusinessPhoneID string `gorm:"type:uuid;not null;index"`
	TemplateID      string `gorm:"type:uuid;not null"`
	TemplateName    string `gorm:"size:512"`
	Language        string `gorm:"size:20"`
	Category        string `gorm:"size:20"`
	ToNumber        string `gorm:"size:32;index"`

	CampaignID *string `gorm:"type:uuid;index"`
	EntryID    *string `gorm:"type:uuid;index"`

	Status string `gorm:"size:20;not null;default:'pending';index"`

	ChargedMicros     int64  `gorm:"not null;default:0"`
	ProviderMessageID string `gorm:"column:provider_message_id;size:191"`
	ResponseStatus    int    `gorm:"default:0"`
	ErrorCode         int    `gorm:"default:0"`
	ErrorMessage      string `gorm:"type:text"`

	ChargedAt  *time.Time
	SentAt     *time.Time
	RefundedAt *time.Time

	CreatedAt time.Time      `gorm:"autoCreateTime"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime;index"`
	DeletedAt gorm.DeletedAt `gorm:"index"`
}

func (WhatsAppTemplateSend) TableName() string {
	return "whatsapp_template_sends"
}

func (s *WhatsAppTemplateSend) BeforeCreate(tx *gorm.DB) error {
	if s.ID == "" {
		s.ID = uuid.New().String()
	}
	return nil
}
