package schema

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type ShippingProviderAccount struct {
	ID           string `gorm:"primaryKey;type:uuid"`
	UserID       string `gorm:"not null;type:uuid"`
	Provider     string `gorm:"not null;size:64"`
	ExternalID   string `gorm:"size:255"`
	Label        string `gorm:"size:255"`
	AccessToken  string `gorm:"not null;size:2048"`
	RefreshToken string `gorm:"size:2048"`
	TokenType    string `gorm:"size:64"`
	Scopes       []byte `gorm:"type:jsonb"`
	ExpiresAt    *time.Time
	AppSettings  []byte         `gorm:"type:jsonb"`
	CreatedAt    time.Time      `gorm:"autoCreateTime"`
	UpdatedAt    time.Time      `gorm:"autoUpdateTime"`
	DeletedAt    gorm.DeletedAt `gorm:"index"`
}

func (ShippingProviderAccount) TableName() string {
	return "shipping_provider_accounts"
}

func (a *ShippingProviderAccount) BeforeCreate(tx *gorm.DB) error {
	if a.ID == "" {
		a.ID = uuid.New().String()
	}
	return nil
}
