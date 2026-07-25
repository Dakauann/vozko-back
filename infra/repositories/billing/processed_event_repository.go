package billing_repository

import (
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"vozko/domain/billing"
	"vozko/infra/database/schema"
)

type processedEventRepository struct {
	db *gorm.DB
}

// NewProcessedEventRepository returns the durable webhook-dedup store backed by Postgres.
func NewProcessedEventRepository(db *gorm.DB) billing.ProcessedEventRepository {
	return &processedEventRepository{db: db}
}

func (r *processedEventRepository) MarkProcessed(provider, eventID string, at time.Time) (bool, error) {
	row := schema.ProcessedWebhookEvent{
		Provider:    provider,
		EventID:     eventID,
		ProcessedAt: at,
	}
	// ON CONFLICT DO NOTHING: a redelivery of the same (provider, event_id) inserts zero rows, so
	// RowsAffected distinguishes the first handling (1) from a duplicate (0) atomically.
	res := r.db.Clauses(clause.OnConflict{DoNothing: true}).Create(&row)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected == 1, nil
}

var _ billing.ProcessedEventRepository = (*processedEventRepository)(nil)
