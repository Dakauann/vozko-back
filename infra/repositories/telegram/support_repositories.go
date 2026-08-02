package telegram_repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	tgdomain "vozko/domain/telegram"
	"vozko/infra/database/schema"
)

// ---------------------------------------------------------------- deep links

type deepLinkRepository struct {
	db *gorm.DB
}

// NewDeepLinkRepository builds the deep-link attribution store.
func NewDeepLinkRepository(db *gorm.DB) tgdomain.DeepLinkRepository {
	return &deepLinkRepository{db: db}
}

func (r *deepLinkRepository) Create(ctx context.Context, d *tgdomain.DeepLink) error {
	record := &schema.TelegramDeepLink{
		Token:        d.Token,
		AccountID:    d.AccountID,
		WorkspaceID:  d.WorkspaceID,
		LeadID:       d.LeadID,
		CampaignID:   d.CampaignID,
		AgentID:      d.AgentID,
		DepartmentID: d.DepartmentID,
		Label:        d.Label,
		ExpiresAt:    d.ExpiresAt,
	}
	if err := r.db.WithContext(ctx).Create(record).Error; err != nil {
		return err
	}
	d.CreatedAt = record.CreatedAt
	return nil
}

func (r *deepLinkRepository) FindByToken(ctx context.Context, token string) (*tgdomain.DeepLink, error) {
	var record schema.TelegramDeepLink
	if err := r.db.WithContext(ctx).First(&record, "token = ?", token).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, tgdomain.ErrDeepLinkNotFound
		}
		return nil, err
	}
	return toDeepLinkDomain(&record), nil
}

func (r *deepLinkRepository) ListByAccount(ctx context.Context, accountID string, limit int) ([]*tgdomain.DeepLink, error) {
	if limit <= 0 {
		limit = 50
	}
	var records []schema.TelegramDeepLink
	if err := r.db.WithContext(ctx).
		Where("account_id = ?", accountID).
		Order("created_at DESC").
		Limit(limit).
		Find(&records).Error; err != nil {
		return nil, err
	}
	out := make([]*tgdomain.DeepLink, 0, len(records))
	for i := range records {
		out = append(out, toDeepLinkDomain(&records[i]))
	}
	return out, nil
}

// MarkUsed counts a redemption rather than consuming the link: a QR code on a
// printed invoice is scanned many times and must keep working.
func (r *deepLinkRepository) MarkUsed(ctx context.Context, token string, at time.Time) error {
	return r.db.WithContext(ctx).Model(&schema.TelegramDeepLink{}).
		Where("token = ?", token).
		Updates(map[string]any{
			"used_at":   gorm.Expr("COALESCE(used_at, ?)", at),
			"use_count": gorm.Expr("use_count + 1"),
		}).Error
}

func (r *deepLinkRepository) Delete(ctx context.Context, accountID, token string) error {
	result := r.db.WithContext(ctx).
		Delete(&schema.TelegramDeepLink{}, "account_id = ? AND token = ?", accountID, token)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return tgdomain.ErrDeepLinkNotFound
	}
	return nil
}

func toDeepLinkDomain(record *schema.TelegramDeepLink) *tgdomain.DeepLink {
	return &tgdomain.DeepLink{
		Token:        record.Token,
		AccountID:    record.AccountID,
		WorkspaceID:  record.WorkspaceID,
		LeadID:       record.LeadID,
		CampaignID:   record.CampaignID,
		AgentID:      record.AgentID,
		DepartmentID: record.DepartmentID,
		Label:        record.Label,
		ExpiresAt:    record.ExpiresAt,
		UsedAt:       record.UsedAt,
		UseCount:     record.UseCount,
		CreatedAt:    record.CreatedAt,
	}
}

// ---------------------------------------------------------------- file cache

type fileCacheRepository struct {
	db *gorm.DB
}

// NewFileCacheRepository builds the object-key → file_id cache.
func NewFileCacheRepository(db *gorm.DB) tgdomain.FileCacheRepository {
	return &fileCacheRepository{db: db}
}

func (r *fileCacheRepository) Get(ctx context.Context, accountID, sourceKey string) (string, error) {
	var fileID string
	err := r.db.WithContext(ctx).Model(&schema.TelegramFileCache{}).
		Where("account_id = ? AND source_key = ?", accountID, sourceKey).
		Limit(1).
		Pluck("file_id", &fileID).Error
	if err != nil {
		return "", err
	}
	return fileID, nil
}

// Put records the id Telegram assigned to an uploaded asset.
//
// A conflict is an update rather than an error: Telegram may hand out a
// different valid file_id for the same content ("a file can have different valid
// file_ids even for the same bot"), and the newest one is always usable.
func (r *fileCacheRepository) Put(ctx context.Context, accountID, sourceKey, fileID string) error {
	if fileID == "" || sourceKey == "" {
		return nil
	}
	return r.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "account_id"}, {Name: "source_key"}},
			DoUpdates: clause.AssignmentColumns([]string{"file_id", "updated_at"}),
		}).
		Create(&schema.TelegramFileCache{
			AccountID: accountID,
			SourceKey: sourceKey,
			FileID:    fileID,
		}).Error
}

// ---------------------------------------------------------------- dedup

type processedEventRepository struct {
	db *gorm.DB
}

// NewProcessedEventRepository builds the durable webhook dedup store.
//
// It shares the webhook_processed_events table with Instagram: the guarantee
// needed is identical (at-least-once delivery, survive a Redis eviction), and a
// per-channel table would duplicate the purge cron for nothing.
func NewProcessedEventRepository(db *gorm.DB) tgdomain.ProcessedEventRepository {
	return &processedEventRepository{db: db}
}

func (r *processedEventRepository) Claim(ctx context.Context, key, channel, accountID string) (bool, error) {
	if key == "" {
		return true, nil
	}
	result := r.db.WithContext(ctx).
		Clauses(clause.OnConflict{DoNothing: true}).
		Create(&schema.WebhookProcessedEvent{
			ID:        key,
			Channel:   channel,
			AccountID: accountID,
		})
	if result.Error != nil {
		return false, result.Error
	}
	// No row inserted means the key was already claimed by an earlier delivery.
	return result.RowsAffected > 0, nil
}

func (r *processedEventRepository) PurgeOlderThan(ctx context.Context, cutoff time.Time) (int64, error) {
	result := r.db.WithContext(ctx).
		Where("channel = ? AND created_at < ?", "telegram", cutoff).
		Delete(&schema.WebhookProcessedEvent{})
	return result.RowsAffected, result.Error
}
