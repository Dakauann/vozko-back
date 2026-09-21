package auth

import "time"

type Session struct {
	ID                       string
	UserID                   string
	RefreshTokenHash         string
	PreviousRefreshTokenHash string
	RotatedAt                *time.Time
	AccessJTI                string
	DeviceInfo               string
	IPAddress                string
	Location                 string
	ExpiresAt                time.Time
	CreatedAt                time.Time
	RevokedAt                *time.Time
}

func (s *Session) IsRevoked() bool {
	return s.RevokedAt != nil
}

func (s *Session) IsExpired() bool {
	return time.Now().After(s.ExpiresAt)
}

type SessionRepository interface {
	Create(session *Session) error
	FindByID(id string) (*Session, error)
	FindByRefreshTokenHash(hash string) (*Session, error)
	FindByPreviousRefreshTokenHash(hash string) (*Session, error)
	FindByAccessJTI(userID string, jti string) (*Session, error)
	FindActiveByUserID(userID string) ([]*Session, error)
	UpdateRefreshToken(sessionID string, expectedTokenHash string, newTokenHash string, newAccessJTI string, newExpiresAt time.Time) (int64, error)
	UpdateSessionInfo(sessionID string, ipAddress string, deviceInfo string) error
	Revoke(sessionID string) error
	RevokeAllByUserID(userID string) error
	DeleteExpired() error
}
