package workspace_config_repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	ca "vozko/domain/audience"
	"vozko/infra/database/schema"
)

// AudienceSettingsStore implements audience.WorkspaceSettingsStore, and the
// analysis sweep's debounce policy, over the workspace configuration row.
//
// The audience engine's two workspace-level settings, the rolling ceiling and
// the debounce window, live on workspace_configs because that is already the
// platform's one row of per-workspace configuration. A settings table of the
// engine's own would have been the same mechanism built a second time.
//
// It lives in this package, beside the repository that owns the row, because
// that is what an outbound port's implementation is. The audience domain never
// learns that workspace_config exists; it declares the two methods it needs and
// this answers them.
//
// What it does NOT share is the permission. The workspace-config use cases are
// gated on admin-or-owner; these two fields are written through the audience
// routes under audience:update, because they govern analysis and nothing else.
type AudienceSettingsStore struct{ db *gorm.DB }

// NewAudienceSettingsStore reads and writes the audience engine's per-workspace
// settings.
func NewAudienceSettingsStore(db *gorm.DB) *AudienceSettingsStore {
	return &AudienceSettingsStore{db: db}
}

// Get reads the two columns, zeroes when the workspace has no configuration row.
//
// A missing row is not an error: it is every workspace that has never opened a
// settings screen, and zero is the value both callers already resolve against
// their own default.
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

// Save writes ONLY these two columns.
//
// A targeted UPDATE rather than reading the record and writing it back whole.
// The row is shared with the roulette policy, the auto-close windows and the
// working hours, and a read-modify-write here would lose any of those that
// changed in between. Creating the row when it is missing goes through the
// repository's own EnsureExists, so the platform defaults for every other
// column come from the one place that knows them.
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

// ConfiguredDebounceWindows lists ONLY the workspaces that changed their quiet
// period, as durations.
//
// The sweep drives off this, which is what makes the setting free for everyone
// else: a deployment where nobody changed it gets an empty map from one indexed
// read, and the sweep then never has to work out which workspace an entry
// belongs to. It is also why clearing the setting stops the special treatment on
// the next tick, with no cleanup pass and no flag to unset anywhere.
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
		// Clamped on the way out, like every other value this row hands back: a
		// number edited by hand must not be able to stop the sweep.
		out[row.WorkspaceID] = ca.DebounceWindow(row.AudienceDebounceMinutes)
	}
	return out, nil
}
