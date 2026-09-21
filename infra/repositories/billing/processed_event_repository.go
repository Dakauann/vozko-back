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

func NewProcessedEventRepository(db *gorm.DB) billing.ProcessedEventRepository {
	return &processedEventRepository{db: db}
}

func (r *processedEventRepository) MarkProcessed(provider, eventID string, at time.Time) (bool, error) {
	row := schema.ProcessedWebhookEvent{
		Provider:    provider,
		EventID:     eventID,
		ProcessedAt: at,
	}
	res := r.db.Clauses(clause.OnConflict{DoNothing: true}).Create(&row)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected == 1, nil
}

var _ billing.ProcessedEventRepository = (*processedEventRepository)(nil)
