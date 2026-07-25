package schema

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type PasswordResetToken struct {
	ID        string    `gorm:"primaryKey;type:uuid"`
	TokenHash string    `gorm:"not null;index;size:64"`
	UserID    string    `gorm:"not null;type:uuid;index"`
	Email     string    `gorm:"not null;size:255"`
	Attempts  int       `gorm:"not null;default:0"`
	ExpiresAt time.Time `gorm:"not null;index"`
	Used      bool      `gorm:"not null;default:false"`
	CreatedAt time.Time `gorm:"autoCreateTime"`
}

func (PasswordResetToken) TableName() string {
	return "password_reset_tokens"
}

func (t *PasswordResetToken) BeforeCreate(tx *gorm.DB) error {
	if t.ID == "" {
		t.ID = uuid.New().String()
	}
	return nil
}
