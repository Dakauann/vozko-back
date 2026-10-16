package whatsapp_repository

import (
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"vozko/domain/whatsapp/quality"
	"vozko/infra/database/schema"
)

type QualityChangeRepository struct {
	db *gorm.DB
}

func NewQualityChangeRepository(db *gorm.DB) *QualityChangeRepository {
	return &QualityChangeRepository{db: db}
}

var (
	_ quality.Recorder = (*QualityChangeRepository)(nil)
	_ quality.History  = (*QualityChangeRepository)(nil)
)

func (r *QualityChangeRepository) LatestValues(subject quality.Subject, signal quality.Signal, externalIDs []string) (map[string]string, error) {
	latest := make(map[string]string, len(externalIDs))
	if len(externalIDs) == 0 {
		return latest, nil
	}
	var rows []struct {
		ExternalID string
		Current    string
	}
	if err := r.db.Raw(`
		SELECT DISTINCT ON (external_id) external_id, current FROM whatsapp_quality_changes
		WHERE subject = ? AND signal = ? AND external_id IN ?
		ORDER BY external_id, occurred_at DESC, recorded_at DESC`,
		string(subject), string(signal), externalIDs,
	).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("read latest whatsapp quality values: %w", err)
	}
	for _, row := range rows {
		latest[row.ExternalID] = row.Current
	}
	return latest, nil
}

func (r *QualityChangeRepository) Record(changes ...quality.Change) error {
	if len(changes) == 0 {
		return nil
	}
	rows := make([]schema.WhatsAppQualityChange, 0, len(changes))
	for _, c := range changes {
		if err := c.Validate(); err != nil {
			return err
		}
		rows = append(rows, schema.WhatsAppQualityChange{
			Subject: string(c.Subject), ExternalID: c.ExternalID, WABAID: c.WABAID, Name: c.Name,
			Signal: string(c.Signal), Event: c.Event, Previous: c.Previous, Current: c.Current,
			Source: string(c.Source), OccurredAt: c.OccurredAt.UTC(),
		})
	}
	if err := r.db.Clauses(clause.OnConflict{DoNothing: true}).Create(&rows).Error; err != nil {
		return fmt.Errorf("record whatsapp quality changes: %w", err)
	}
	return nil
}
