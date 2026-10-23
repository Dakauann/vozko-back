package schema

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"vozko/infra/crypto/piigorm"
)

type GoogleAccount struct {
	ID          string `gorm:"primaryKey;type:uuid"`
	WorkspaceID string `gorm:"type:uuid;not null"`
	Email       string `gorm:"size:320;not null"`

	AccessToken     piigorm.EncryptedString `gorm:"type:bytea" json:"-"`
	RefreshToken    piigorm.EncryptedString `gorm:"type:bytea" json:"-"`
	AccessExpiresAt *time.Time              `gorm:"type:timestamptz"`
	Scopes          string                  `gorm:"type:text;not null;default:''"`

	Status       string  `gorm:"size:24;not null;default:'disconnected'"`
	StatusReason string  `gorm:"size:255"`
	ConnectedBy  *string `gorm:"type:uuid"`

	CreatedAt time.Time `gorm:"autoCreateTime"`
	UpdatedAt time.Time `gorm:"autoUpdateTime"`
}

func (GoogleAccount) TableName() string { return "google_accounts" }

func (a *GoogleAccount) BeforeCreate(tx *gorm.DB) error {
	if a.ID == "" {
		a.ID = uuid.New().String()
	}
	return nil
}

type GoogleAccountBinding struct {
	WorkspaceID string    `gorm:"primaryKey;type:uuid"`
	Product     string    `gorm:"primaryKey;size:16"`
	AccountID   string    `gorm:"type:uuid;not null;index:idx_google_binding_account"`
	CreatedAt   time.Time `gorm:"autoCreateTime"`
	UpdatedAt   time.Time `gorm:"autoUpdateTime"`
}

func (GoogleAccountBinding) TableName() string { return "google_account_bindings" }

func GoogleModels() []any {
	return []any{&GoogleAccount{}, &GoogleAccountBinding{}}
}
