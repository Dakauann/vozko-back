package workspace_config_repository

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"gorm.io/gorm"

	wsc "vozko/domain/workspace_config"
	"vozko/infra/database/schema"
)

type Repository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) wsc.Repository {
	return &Repository{db: db}
}

func (r *Repository) GetByWorkspaceID(ctx context.Context, workspaceID string) (*wsc.WorkspaceConfig, error) {
	var row schema.WorkspaceConfig
	if err := r.db.WithContext(ctx).Where("workspace_id = ?", workspaceID).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {

			return &wsc.WorkspaceConfig{
				WorkspaceID:                workspaceID,
				CampaignSpamProtectionDays: wsc.DefaultCampaignSpamProtectionDays,
				SkipAdminAssignment:        false,
				AutoCloseEnabled:           wsc.DefaultAutoCloseEnabled,
				AutoCloseIdleAfterHours:    wsc.DefaultAutoCloseIdleAfterHours,
				AutoCloseMaxAgeEnabled:     wsc.DefaultAutoCloseMaxAgeEnabled,
				AutoCloseMaxAgeAfterHours:  wsc.DefaultAutoCloseMaxAgeAfterHours,
			}, nil
		}
		return nil, err
	}
	idleHours := row.AutoCloseIdleAfterHours
	if idleHours <= 0 {
		idleHours = wsc.DefaultAutoCloseIdleAfterHours
	}
	maxAgeHours := row.AutoCloseMaxAgeAfterHours
	if maxAgeHours <= 0 {
		maxAgeHours = wsc.DefaultAutoCloseMaxAgeAfterHours
	}
	return &wsc.WorkspaceConfig{
		ID:                                  row.ID,
		WorkspaceID:                         row.WorkspaceID,
		CampaignSpamProtectionDays:          row.CampaignSpamProtectionDays,
		SkipAdminAssignment:                 row.SkipAdminAssignment,
		IncludedUnofficialWhatsAppInstances: row.IncludedUnofficialWhatsAppInstances,
		HoldMusicTrack:                      row.HoldMusicTrack,
		QueueEnabled:                        row.QueueEnabled,
		QueueMaxWaitSeconds:                 row.QueueMaxWaitSeconds,
		QueueMaxLength:                      row.QueueMaxLength,
		QueueOverflow:                       row.QueueOverflow,
		AutoCloseEnabled:                    row.AutoCloseEnabled,
		AutoCloseIdleAfterHours:             idleHours,
		AutoCloseMaxAgeEnabled:              row.AutoCloseMaxAgeEnabled,
		AutoCloseMaxAgeAfterHours:           maxAgeHours,
		UpdatedBy:                           row.UpdatedBy,
		CreatedAt:                           row.CreatedAt,
		UpdatedAt:                           row.UpdatedAt,
	}, nil
}

func (r *Repository) Upsert(ctx context.Context, cfg *wsc.WorkspaceConfig) error {
	idleHours := wsc.ClampAutoCloseIdleHours(cfg.AutoCloseIdleAfterHours)
	maxAgeHours := wsc.ClampAutoCloseMaxAgeHours(cfg.AutoCloseMaxAgeAfterHours)
	row := &schema.WorkspaceConfig{
		ID:                                  cfg.ID,
		WorkspaceID:                         cfg.WorkspaceID,
		CampaignSpamProtectionDays:          cfg.CampaignSpamProtectionDays,
		SkipAdminAssignment:                 cfg.SkipAdminAssignment,
		IncludedUnofficialWhatsAppInstances: cfg.IncludedUnofficialWhatsAppInstances,
		HoldMusicTrack:                      cfg.HoldMusicTrack,
		QueueEnabled:                        cfg.QueueEnabled,
		QueueMaxWaitSeconds:                 cfg.QueueMaxWaitSeconds,
		QueueMaxLength:                      cfg.QueueMaxLength,
		QueueOverflow:                       cfg.QueueOverflow,
		AutoCloseEnabled:                    cfg.AutoCloseEnabled,
		AutoCloseIdleAfterHours:             idleHours,
		AutoCloseMaxAgeEnabled:              cfg.AutoCloseMaxAgeEnabled,
		AutoCloseMaxAgeAfterHours:           maxAgeHours,
		UpdatedBy:                           cfg.UpdatedBy,
	}
	if row.ID == "" {
		row.ID = uuid.New().String()
	}
	return r.db.WithContext(ctx).Save(row).Error
}

func (r *Repository) EnsureExists(ctx context.Context, workspaceID string) error {
	var count int64
	if err := r.db.WithContext(ctx).Model(&schema.WorkspaceConfig{}).Where("workspace_id = ?", workspaceID).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	row := &schema.WorkspaceConfig{
		ID:                         uuid.New().String(),
		WorkspaceID:                workspaceID,
		CampaignSpamProtectionDays: wsc.DefaultCampaignSpamProtectionDays,
		AutoCloseEnabled:           wsc.DefaultAutoCloseEnabled,
		AutoCloseIdleAfterHours:    wsc.DefaultAutoCloseIdleAfterHours,
		AutoCloseMaxAgeEnabled:     wsc.DefaultAutoCloseMaxAgeEnabled,
		AutoCloseMaxAgeAfterHours:  wsc.DefaultAutoCloseMaxAgeAfterHours,
	}
	return r.db.WithContext(ctx).Omit("UpdatedBy").Create(row).Error
}

// GetIncludedUnofficialInstancesByWorkspaceIDs reads the granted allowance for
// many workspaces in one query.
//
// Only workspaces with a config row appear in the result; a missing entry means
// zero, which is what the caller must already assume for a workspace that has
// never been granted anything. Returning explicit zeros instead would make the
// map larger without making it more informative.
func (r *Repository) GetIncludedUnofficialInstancesByWorkspaceIDs(
	ctx context.Context,
	workspaceIDs []string,
) (map[string]int, error) {
	out := make(map[string]int, len(workspaceIDs))
	if len(workspaceIDs) == 0 {
		return out, nil
	}

	type row struct {
		WorkspaceID string `gorm:"column:workspace_id"`
		Included    int    `gorm:"column:included_unofficial_whatsapp_instances"`
	}
	var rows []row
	err := r.db.WithContext(ctx).
		Model(&schema.WorkspaceConfig{}).
		Select("workspace_id, included_unofficial_whatsapp_instances").
		Where("workspace_id IN ?", workspaceIDs).
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, item := range rows {
		out[item.WorkspaceID] = item.Included
	}
	return out, nil
}
