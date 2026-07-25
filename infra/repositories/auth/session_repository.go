package auth_repository

import (
	"errors"
	"time"

	"vozko/domain/auth"
	"vozko/infra/database/schema"

	"gorm.io/gorm"
)

type SessionRepositoryImpl struct {
	db *gorm.DB
}

func NewSessionRepository(db *gorm.DB) auth.SessionRepository {
	return &SessionRepositoryImpl{db: db}
}

func (r *SessionRepositoryImpl) Create(session *auth.Session) error {
	dbSession := &schema.Session{
		ID:                       session.ID,
		UserID:                   session.UserID,
		RefreshTokenHash:         session.RefreshTokenHash,
		PreviousRefreshTokenHash: session.PreviousRefreshTokenHash,
		RotatedAt:                session.RotatedAt,
		AccessJTI:                session.AccessJTI,
		DeviceInfo:               session.DeviceInfo,
		IPAddress:                session.IPAddress,
		Location:                 session.Location,
		ExpiresAt:                session.ExpiresAt,
		RevokedAt:                session.RevokedAt,
	}
	if err := r.db.Create(dbSession).Error; err != nil {
		return err
	}
	session.ID = dbSession.ID
	session.CreatedAt = dbSession.CreatedAt
	return nil
}

func (r *SessionRepositoryImpl) FindByID(id string) (*auth.Session, error) {
	var s schema.Session
	if err := r.db.Where("id = ?", id).First(&s).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, auth.ErrSessionNotFound
		}
		return nil, err
	}
	return mapSessionToDomain(&s), nil
}

func (r *SessionRepositoryImpl) FindByRefreshTokenHash(hash string) (*auth.Session, error) {
	var s schema.Session
	if err := r.db.Where("refresh_token_hash = ?", hash).First(&s).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, auth.ErrSessionNotFound
		}
		return nil, err
	}
	return mapSessionToDomain(&s), nil
}

func (r *SessionRepositoryImpl) FindByPreviousRefreshTokenHash(hash string) (*auth.Session, error) {
	var s schema.Session
	if err := r.db.Where("previous_refresh_token_hash = ?", hash).First(&s).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, auth.ErrSessionNotFound
		}
		return nil, err
	}
	return mapSessionToDomain(&s), nil
}

func (r *SessionRepositoryImpl) FindByAccessJTI(userID string, jti string) (*auth.Session, error) {
	var s schema.Session
	if err := r.db.Where("user_id = ? AND access_jti = ? AND revoked_at IS NULL", userID, jti).First(&s).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, auth.ErrSessionNotFound
		}
		return nil, err
	}
	return mapSessionToDomain(&s), nil
}

func (r *SessionRepositoryImpl) FindActiveByUserID(userID string) ([]*auth.Session, error) {
	var sessions []schema.Session
	if err := r.db.Where("user_id = ? AND revoked_at IS NULL AND expires_at > ?", userID, time.Now()).
		Order("created_at DESC").
		Find(&sessions).Error; err != nil {
		return nil, err
	}
	result := make([]*auth.Session, len(sessions))
	for i := range sessions {
		result[i] = mapSessionToDomain(&sessions[i])
	}
	return result, nil
}

func (r *SessionRepositoryImpl) UpdateRefreshToken(sessionID string, expectedTokenHash string, newTokenHash string, newAccessJTI string, newExpiresAt time.Time) (int64, error) {
	now := time.Now().UTC()
	updates := map[string]interface{}{
		"refresh_token_hash":          newTokenHash,
		"previous_refresh_token_hash": expectedTokenHash,
		"rotated_at":                  &now,
		"access_jti":                  newAccessJTI,
		"expires_at":                  newExpiresAt,
	}
	// Compare-and-swap: the row is only rotated while it still holds the token the
	// caller read. A concurrent rotation changes refresh_token_hash first and this
	// UPDATE matches nothing (RowsAffected == 0), so the loser never overwrites the
	// winner's freshly issued token.
	res := r.db.Model(&schema.Session{}).
		Where("id = ? AND refresh_token_hash = ?", sessionID, expectedTokenHash).
		Updates(updates)
	return res.RowsAffected, res.Error
}

func (r *SessionRepositoryImpl) UpdateSessionInfo(sessionID string, ipAddress string, deviceInfo string) error {
	updates := map[string]interface{}{}
	if ipAddress != "" {
		updates["ip_address"] = ipAddress
	}
	if deviceInfo != "" {
		updates["device_info"] = deviceInfo
	}
	if len(updates) == 0 {
		return nil
	}
	return r.db.Model(&schema.Session{}).Where("id = ?", sessionID).Updates(updates).Error
}

func (r *SessionRepositoryImpl) Revoke(sessionID string) error {
	now := time.Now()
	return r.db.Model(&schema.Session{}).Where("id = ?", sessionID).Update("revoked_at", &now).Error
}

func (r *SessionRepositoryImpl) RevokeAllByUserID(userID string) error {
	now := time.Now()
	return r.db.Model(&schema.Session{}).
		Where("user_id = ? AND revoked_at IS NULL", userID).
		Update("revoked_at", &now).Error
}

func (r *SessionRepositoryImpl) DeleteExpired() error {
	return r.db.Where("expires_at < ?", time.Now()).Delete(&schema.Session{}).Error
}

func mapSessionToDomain(s *schema.Session) *auth.Session {
	return &auth.Session{
		ID:                       s.ID,
		UserID:                   s.UserID,
		RefreshTokenHash:         s.RefreshTokenHash,
		PreviousRefreshTokenHash: s.PreviousRefreshTokenHash,
		RotatedAt:                s.RotatedAt,
		AccessJTI:                s.AccessJTI,
		DeviceInfo:               s.DeviceInfo,
		IPAddress:                s.IPAddress,
		Location:                 s.Location,
		ExpiresAt:                s.ExpiresAt,
		CreatedAt:                s.CreatedAt,
		RevokedAt:                s.RevokedAt,
	}
}
