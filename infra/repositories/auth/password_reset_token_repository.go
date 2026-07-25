package auth_repository

import (
	"errors"
	"time"

	"vozko/domain/auth"
	"vozko/infra/database/schema"

	"gorm.io/gorm"
)

type PasswordResetTokenRepository struct {
	db *gorm.DB
}

func NewPasswordResetTokenRepository(db *gorm.DB) auth.PasswordResetTokenRepository {
	return &PasswordResetTokenRepository{db: db}
}

func (r *PasswordResetTokenRepository) Create(token *auth.PasswordResetToken) error {
	dbToken := &schema.PasswordResetToken{
		ID:        token.ID,
		TokenHash: token.TokenHash,
		UserID:    token.UserID,
		Email:     token.Email,
		Attempts:  token.Attempts,
		ExpiresAt: token.ExpiresAt,
		Used:      token.Used,
		CreatedAt: token.CreatedAt,
	}
	if err := r.db.Create(dbToken).Error; err != nil {
		return err
	}
	token.ID = dbToken.ID
	return nil
}

func (r *PasswordResetTokenRepository) FindActiveByUserID(userID string) (*auth.PasswordResetToken, error) {
	var dbToken schema.PasswordResetToken
	err := r.db.
		Where("user_id = ? AND used = ? AND expires_at > ?", userID, false, time.Now().UTC()).
		Order("created_at DESC").
		First(&dbToken).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, auth.ErrInvalidResetToken
		}
		return nil, err
	}
	return &auth.PasswordResetToken{
		ID:        dbToken.ID,
		TokenHash: dbToken.TokenHash,
		UserID:    dbToken.UserID,
		Email:     dbToken.Email,
		Attempts:  dbToken.Attempts,
		ExpiresAt: dbToken.ExpiresAt,
		Used:      dbToken.Used,
		CreatedAt: dbToken.CreatedAt,
	}, nil
}

func (r *PasswordResetTokenRepository) IncrementAttempts(id string) (int, error) {
	if err := r.db.Model(&schema.PasswordResetToken{}).
		Where("id = ?", id).
		UpdateColumn("attempts", gorm.Expr("attempts + 1")).Error; err != nil {
		return 0, err
	}
	var dbToken schema.PasswordResetToken
	if err := r.db.Select("attempts").Where("id = ?", id).First(&dbToken).Error; err != nil {
		return 0, err
	}
	return dbToken.Attempts, nil
}

func (r *PasswordResetTokenRepository) MarkUsed(id string) error {
	return r.db.Model(&schema.PasswordResetToken{}).
		Where("id = ?", id).
		Update("used", true).Error
}

func (r *PasswordResetTokenRepository) DeleteByUserID(userID string) error {
	return r.db.Where("user_id = ?", userID).
		Delete(&schema.PasswordResetToken{}).Error
}

func (r *PasswordResetTokenRepository) DeleteExpired() error {
	return r.db.Where("expires_at < ? OR used = ?", time.Now().UTC(), true).
		Delete(&schema.PasswordResetToken{}).Error
}
