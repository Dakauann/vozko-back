package instagram_repository

import (
	"context"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	igdomain "vozko/domain/instagram"
	"vozko/infra/database/schema"
)

type processedEventRepository struct {
	db *gorm.DB
}

func NewProcessedEventRepository(db *gorm.DB) igdomain.ProcessedEventRepository {
	return &processedEventRepository{db: db}
}

func (r *processedEventRepository) Claim(ctx context.Context, key, channel, accountID string) (bool, error) {
	record := &schema.WebhookProcessedEvent{
		ID:        key,
		Channel:   channel,
		AccountID: accountID,
	}
	result := r.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "id"}},
			DoNothing: true,
		}).
		Create(record)
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected == 1, nil
}

func (r *processedEventRepository) PurgeOlderThan(ctx context.Context, cutoff time.Time) (int64, error) {
	result := r.db.WithContext(ctx).
		Where("created_at < ?", cutoff).
		Delete(&schema.WebhookProcessedEvent{})
	if result.Error != nil {
		return 0, result.Error
	}
	return result.RowsAffected, nil
}
