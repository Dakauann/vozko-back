package whatsapp_repository

import (
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"vozko/domain/whatsapp/optout"
	"vozko/infra/database/schema"
)

type optOutRepository struct {
	db *gorm.DB
}

func NewOptOutRepository(db *gorm.DB) optout.Recorder {
	return &optOutRepository{db: db}
}

func (r *optOutRepository) Record(events ...optout.Event) error {
	if len(events) == 0 {
		return nil
	}
	rows := make([]schema.WhatsAppOptOutEvent, 0, len(events))
	for _, e := range events {
		if err := e.Validate(); err != nil {
			return err
		}
		rows = append(rows, schema.WhatsAppOptOutEvent{
			PhoneNumberID: e.PhoneNumberID, RecipientHash: e.RecipientHash, Preference: string(e.Preference),
			Category: e.Category, OccurredAt: e.OccurredAt.UTC(),
		})
	}
	if err := r.db.Clauses(clause.OnConflict{DoNothing: true}).Create(&rows).Error; err != nil {
		return fmt.Errorf("record whatsapp opt-out events: %w", err)
	}
	return nil
}
