package schema

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type Session struct {
	ID                       string     `gorm:"primaryKey;type:uuid"`
	UserID                   string     `gorm:"not null;type:uuid;index:idx_sessions_user_id"`
	RefreshTokenHash         string     `gorm:"not null;uniqueIndex;size:64"`
	PreviousRefreshTokenHash string     `gorm:"size:64;index:idx_sessions_prev_refresh_hash"`
	RotatedAt                *time.Time `gorm:""`
	AccessJTI                string     `gorm:"not null;size:36"`
	DeviceInfo               string     `gorm:"size:500"`
	IPAddress                string     `gorm:"size:45"`
	Location                 string     `gorm:"size:200"`
	ExpiresAt                time.Time  `gorm:"not null;index:idx_sessions_expires_at"`
	CreatedAt                time.Time  `gorm:"autoCreateTime"`
	RevokedAt                *time.Time `gorm:"index:idx_sessions_revoked_at"`

	User User `gorm:"foreignKey:UserID;constraint:OnDelete:CASCADE"`
}

func (Session) TableName() string {
	return "sessions"
}

func (s *Session) BeforeCreate(tx *gorm.DB) error {
	if s.ID == "" {
		s.ID = uuid.New().String()
	}
	return nil
}
