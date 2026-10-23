package carriermessaging_repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"vozko/domain/carriermessaging"
	"vozko/infra/database/schema"
)

type settingsRepository struct {
	db *gorm.DB
}

func NewSettingsRepository(db *gorm.DB) carriermessaging.SettingsRepository {
	return &settingsRepository{db: db}
}

func (r *settingsRepository) Get(ctx context.Context, workspaceID string) (*carriermessaging.Settings, error) {
	var record schema.CarrierSettings
	if err := r.db.WithContext(ctx).Where("workspace_id = ?", workspaceID).Take(&record).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	settings := &carriermessaging.Settings{
		WorkspaceID:     record.WorkspaceID,
		FrequencyCap:    carriermessaging.FrequencyCap{PerDay: record.CapPerDay, PerWeek: record.CapPerWeek},
		AttributionDays: record.AttributionDays,
	}
	if err := decodeJSON("quiet hours", record.QuietHours, &settings.QuietHours); err != nil {
		return nil, err
	}
	return settings, nil
}

func (r *settingsRepository) Put(ctx context.Context, s carriermessaging.Settings) error {
	quietHours, err := encodeJSON("quiet hours", s.QuietHours)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	return r.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "workspace_id"}},
			DoUpdates: clause.AssignmentColumns([]string{"quiet_hours", "frequency_cap_per_day", "frequency_cap_per_week", "attribution_days", "updated_at"}),
		}).
		Create(&schema.CarrierSettings{
			WorkspaceID:     s.WorkspaceID,
			QuietHours:      quietHours,
			CapPerDay:       s.FrequencyCap.PerDay,
			CapPerWeek:      s.FrequencyCap.PerWeek,
			AttributionDays: s.AttributionDays,
			CreatedAt:       now,
			UpdatedAt:       now,
		}).Error
}
