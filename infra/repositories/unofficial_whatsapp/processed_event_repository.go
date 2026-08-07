package unofficial_whatsapp_repository

import (
	"context"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	uw "vozko/domain/unofficial_whatsapp"
	"vozko/infra/database/schema"
)

// ChannelKey is the value written to the shared processed-events table.
//
// It scopes this channel's rows so the purge cannot delete another channel's,
// and it matches the entry type so an operator grepping the table finds what
// they expect.
const ChannelKey = "unofficial_whatsapp"

type processedEventRepository struct {
	db *gorm.DB
}

// NewProcessedEventRepository builds the durable webhook dedup store.
//
// It reuses the SHARED webhook_processed_events table rather than adding a
// per-channel one: the guarantee wanted here is exactly the one the other
// channels already have, and a second table would be a second place for the
// retention sweep to forget about.
func NewProcessedEventRepository(db *gorm.DB) uw.ProcessedEventRepository {
	return &processedEventRepository{db: db}
}

// Claim records an event key, reporting whether this caller won the race.
//
// The insert IS the lock: a conflicting key means an earlier delivery already
// claimed it, which is what still rejects a replay after the Redis fast path's
// key has expired or been evicted.
func (r *processedEventRepository) Claim(ctx context.Context, key, channel, instanceID string) (bool, error) {
	if key == "" {
		// An event with no natural id and no payload to digest cannot be
		// deduplicated. Processing it is safer than refusing it: this provider
		// has no replay, so a refusal is permanent loss.
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
