package auth

import "time"

type Session struct {
	ID               string
	UserID           string
	RefreshTokenHash string
	// PreviousRefreshTokenHash holds the hash this session most recently rotated
	// away from. Presenting a token whose hash matches it (after the grace window)
	// means an already-spent refresh token is being replayed: token theft.
	PreviousRefreshTokenHash string
	// RotatedAt is when the current token last replaced the previous one; it bounds
	// the grace window that tolerates an honest client's retry of a dropped refresh.
	RotatedAt  *time.Time
	AccessJTI  string
	DeviceInfo string
	IPAddress  string
	Location   string
	ExpiresAt  time.Time
	CreatedAt  time.Time
	RevokedAt  *time.Time
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
	// FindByPreviousRefreshTokenHash locates a live session whose prior (rotated
	// away) token hash matches, used to distinguish an honest retry from a replay.
	FindByPreviousRefreshTokenHash(hash string) (*Session, error)
	FindByAccessJTI(userID string, jti string) (*Session, error)
	FindActiveByUserID(userID string) ([]*Session, error)
	// UpdateRefreshToken atomically rotates a session's token only if it still
	// holds expectedTokenHash (compare-and-swap), recording the old hash as the
	// previous one. It returns the number of rows changed: 0 means another request
	// already rotated this token, so the caller lost the race.
	UpdateRefreshToken(sessionID string, expectedTokenHash string, newTokenHash string, newAccessJTI string, newExpiresAt time.Time) (int64, error)
	UpdateSessionInfo(sessionID string, ipAddress string, deviceInfo string) error
	Revoke(sessionID string) error
	RevokeAllByUserID(userID string) error
	DeleteExpired() error
}
