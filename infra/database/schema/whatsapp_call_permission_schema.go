package schema

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type WhatsAppCallPermission struct {
	ID              string     `gorm:"primaryKey;type:uuid"`
	WorkspaceID     string     `gorm:"type:uuid;not null;index"`
	BusinessPhoneID string     `gorm:"type:uuid;not null;index;uniqueIndex:uni_phone_user,priority:1"`
	UserNumber      string     `gorm:"size:32;not null;uniqueIndex:uni_phone_user,priority:2"`
	LeadID          *string    `gorm:"type:uuid;index"`
	Status          string     `gorm:"size:16;not null;default:pending"`
	ExpiresAt       *time.Time `gorm:"index"`
	ResponseSource  string     `gorm:"size:16"`
	RequestedAt     *time.Time
	RespondedAt     *time.Time
	CreatedAt       time.Time `gorm:"autoCreateTime"`
	UpdatedAt       time.Time `gorm:"autoUpdateTime"`
}

func (WhatsAppCallPermission) TableName() string {
	return "whatsapp_call_permissions"
}

func (p *WhatsAppCallPermission) BeforeCreate(tx *gorm.DB) error {
	if p.ID == "" {
		p.ID = uuid.New().String()
	}
	return nil
}
