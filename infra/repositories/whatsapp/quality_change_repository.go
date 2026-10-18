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
	_ quality.Timeline  = (*QualityChangeRepository)(nil)
	_ quality.ChangeLog = (*QualityChangeRepository)(nil)
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

func (r *QualityChangeRepository) LatestBySignal(subject quality.Subject, externalID string) (map[quality.Signal]quality.Change, error) {
	var rows []schema.WhatsAppQualityChange
	if err := r.db.Raw(`
		SELECT DISTINCT ON (signal) * FROM whatsapp_quality_changes
		WHERE subject = ? AND external_id = ?
		ORDER BY signal, occurred_at DESC, recorded_at DESC`,
		string(subject), externalID,
	).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("read the whatsapp quality timeline: %w", err)
	}
	latest := make(map[quality.Signal]quality.Change, len(rows))
	for _, row := range rows {
		latest[quality.Signal(row.Signal)] = changeOf(row)
	}
	return latest, nil
}

func (r *QualityChangeRepository) Recent(subject quality.Subject, externalID string, limit int) ([]quality.Change, error) {
	var rows []schema.WhatsAppQualityChange
	if err := r.db.Where("subject = ? AND external_id = ?", string(subject), externalID).
		Order("occurred_at DESC, recorded_at DESC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("read the recent whatsapp quality changes: %w", err)
	}
	changes := make([]quality.Change, len(rows))
	for i, row := range rows {
		changes[i] = changeOf(row)
	}
	return changes, nil
}

func changeOf(row schema.WhatsAppQualityChange) quality.Change {
	return quality.Change{
		Subject: quality.Subject(row.Subject), ExternalID: row.ExternalID, WABAID: row.WABAID, Name: row.Name,
		Signal: quality.Signal(row.Signal), Event: row.Event, Previous: row.Previous, Current: row.Current,
		Source: quality.Source(row.Source), OccurredAt: row.OccurredAt,
	}
}
