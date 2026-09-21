package workspace_config_repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	ca "vozko/domain/audience"
	"vozko/infra/database/schema"
)

type AudienceSettingsStore struct{ db *gorm.DB }

func NewAudienceSettingsStore(db *gorm.DB) *AudienceSettingsStore {
	return &AudienceSettingsStore{db: db}
}

func (s *AudienceSettingsStore) Get(ctx context.Context, workspaceID string) (ca.WorkspaceSettings, error) {
	if workspaceID == "" {
		return ca.WorkspaceSettings{}, nil
	}
	var row schema.WorkspaceConfig
	err := s.db.WithContext(ctx).
		Select("audience_daily_cap", "audience_debounce_minutes").
		Where("workspace_id = ?", workspaceID).
		First(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ca.WorkspaceSettings{}, nil
		}
		return ca.WorkspaceSettings{}, err
	}
	return ca.WorkspaceSettings{
		DailyCap:        row.AudienceDailyCap,
		DebounceMinutes: row.AudienceDebounceMinutes,
	}, nil
}

func (s *AudienceSettingsStore) Save(ctx context.Context, workspaceID string, settings ca.WorkspaceSettings) error {
	if workspaceID == "" {
		return ca.ErrWorkspaceRequired
	}
	if err := (&Repository{db: s.db}).EnsureExists(ctx, workspaceID); err != nil {
		return err
	}
	return s.db.WithContext(ctx).
		Model(&schema.WorkspaceConfig{}).
		Where("workspace_id = ?", workspaceID).
		Updates(map[string]interface{}{
			"audience_daily_cap":        settings.DailyCap,
			"audience_debounce_minutes": settings.DebounceMinutes,
		}).Error
}

func (s *AudienceSettingsStore) ConfiguredDebounceWindows(ctx context.Context) (map[string]time.Duration, error) {
	var rows []schema.WorkspaceConfig
	err := s.db.WithContext(ctx).
		Model(&schema.WorkspaceConfig{}).
		Select("workspace_id", "audience_debounce_minutes").
		Where("audience_debounce_minutes > 0").
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	out := make(map[string]time.Duration, len(rows))
	for _, row := range rows {
		out[row.WorkspaceID] = ca.DebounceWindow(row.AudienceDebounceMinutes)
	}
	return out, nil
}
