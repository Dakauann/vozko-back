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

// NewProcessedEventRepository builds the durable webhook dedup store.
//
// It follows the pattern already proven by telemetry_dedupe. The reason it exists
// alongside the Redis guard: the Redis dedup key has a 5-minute TTL and its
// one-shot variant fails OPEN on a Redis error, so an at-least-once redelivery
// arriving after eviction — or during a Redis outage — would be processed twice.
// Postgres is the durable backstop.
func NewProcessedEventRepository(db *gorm.DB) igdomain.ProcessedEventRepository {
	return &processedEventRepository{db: db}
}

// Claim inserts the idempotency key, returning false when it was already present.
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

// PurgeOlderThan trims processed keys. Called from cron; the retention window
// only needs to outlive Meta's redelivery horizon.
func (r *processedEventRepository) PurgeOlderThan(ctx context.Context, cutoff time.Time) (int64, error) {
	result := r.db.WithContext(ctx).
		Where("created_at < ?", cutoff).
		Delete(&schema.WebhookProcessedEvent{})
	if result.Error != nil {
		return 0, result.Error
	}
	return result.RowsAffected, nil
}
