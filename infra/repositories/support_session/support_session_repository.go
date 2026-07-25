package support_session_repository

import (
	"time"

	"gorm.io/gorm"

	si "vozko/domain/support_inbox"
	"vozko/infra/database/schema"
)

type repository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) si.SessionRepository {
	return &repository{db: db}
}

func (r *repository) Create(session *si.SupportSession) error {
	record := schema.SupportSession{
		ID:        session.ID,
		InboxID:   session.InboxID,
		EntryID:   session.EntryID,
		Token:     session.Token,
		ExpiresAt: session.ExpiresAt,
	}
	return r.db.Create(&record).Error
}

func (r *repository) FindByToken(token string) (*si.SupportSession, error) {
	var record schema.SupportSession
	if err := r.db.Where("token = ?", token).First(&record).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, si.ErrSessionNotFound
		}
		return nil, err
	}
	return mapToDomain(&record), nil
}

func (r *repository) DeleteExpired() (int64, error) {
	result := r.db.Where("expires_at < ?", time.Now()).Delete(&schema.SupportSession{})
	return result.RowsAffected, result.Error
}

func mapToDomain(record *schema.SupportSession) *si.SupportSession {
	return &si.SupportSession{
		ID:        record.ID,
		InboxID:   record.InboxID,
		EntryID:   record.EntryID,
		Token:     record.Token,
		ExpiresAt: record.ExpiresAt,
		CreatedAt: record.CreatedAt,
	}
}
