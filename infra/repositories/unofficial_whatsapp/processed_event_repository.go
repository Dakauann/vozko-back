package unofficial_whatsapp_repository

import (
	"context"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	uw "vozko/domain/unofficial_whatsapp"
	"vozko/infra/database/schema"
)

const ChannelKey = "unofficial_whatsapp"

type processedEventRepository struct {
	db *gorm.DB
}

func NewProcessedEventRepository(db *gorm.DB) uw.ProcessedEventRepository {
	return &processedEventRepository{db: db}
}

func (r *processedEventRepository) Claim(ctx context.Context, key, channel, instanceID string) (bool, error) {
	if key == "" {
		return true, nil
	}
	if channel == "" {
		channel = ChannelKey
	}

	result := r.db.WithContext(ctx).
		Clauses(clause.OnConflict{DoNothing: true}).
		Create(&schema.WebhookProcessedEvent{
			ID:        key,
			Channel:   channel,
			AccountID: instanceID,
		})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

func (r *processedEventRepository) PurgeOlderThan(ctx context.Context, cutoff time.Time) (int64, error) {
	result := r.db.WithContext(ctx).
		Where("channel = ? AND created_at < ?", ChannelKey, cutoff).
		Delete(&schema.WebhookProcessedEvent{})
	return result.RowsAffected, result.Error
}
