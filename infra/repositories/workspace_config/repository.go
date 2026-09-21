package workspace_config_repository

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"vozko/domain/working_hours"
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

				RouletteMode:                wsc.DefaultRouletteMode,
				RouletteLastSeenWindowHours: wsc.DefaultRouletteLastSeenWindowHours,
				RouletteRescueEnabled:       wsc.DefaultRouletteRescueEnabled,
				RouletteRescueAfterMinutes:  wsc.DefaultRouletteRescueAfterMinutes,
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
		AutoCloseEnabled:                    row.AutoCloseEnabled,
		AutoCloseIdleAfterHours:             idleHours,
		AutoCloseMaxAgeEnabled:              row.AutoCloseMaxAgeEnabled,
		AutoCloseMaxAgeAfterHours:           maxAgeHours,

		RouletteMode:                wsc.NormalizeRouletteMode(row.RouletteMode),
		RouletteLastSeenWindowHours: wsc.ClampRouletteLastSeenWindowHours(row.RouletteLastSeenWindowHours),
		RouletteRescueEnabled:       row.RouletteRescueEnabled,
		RouletteRescueAfterMinutes:  wsc.ClampRouletteRescueMinutes(row.RouletteRescueAfterMinutes),

		WorkingHours: decodeWorkingHours(row.WorkspaceID, row.WorkingHours),

		AudienceDailyCap:        row.AudienceDailyCap,
		AudienceDebounceMinutes: row.AudienceDebounceMinutes,

		UpdatedBy: row.UpdatedBy,
		CreatedAt: row.CreatedAt,
		UpdatedAt: row.UpdatedAt,
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
		AutoCloseEnabled:                    cfg.AutoCloseEnabled,
		AutoCloseIdleAfterHours:             idleHours,
		AutoCloseMaxAgeEnabled:              cfg.AutoCloseMaxAgeEnabled,
		AutoCloseMaxAgeAfterHours:           maxAgeHours,

		RouletteMode:                wsc.NormalizeRouletteMode(cfg.RouletteMode),
		RouletteLastSeenWindowHours: wsc.ClampRouletteLastSeenWindowHours(cfg.RouletteLastSeenWindowHours),
		RouletteRescueEnabled:       cfg.RouletteRescueEnabled,
		RouletteRescueAfterMinutes:  wsc.ClampRouletteRescueMinutes(cfg.RouletteRescueAfterMinutes),

		AudienceDailyCap:        cfg.AudienceDailyCap,
		AudienceDebounceMinutes: cfg.AudienceDebounceMinutes,

		UpdatedBy: cfg.UpdatedBy,
	}

	encoded, err := working_hours.EncodeSpec(cfg.WorkingHours)
	if err != nil {
		return err
	}
	row.WorkingHours = encoded

	if row.ID == "" {
		row.ID = uuid.New().String()
	}
	return r.db.WithContext(ctx).Save(row).Error
}

func decodeWorkingHours(workspaceID string, raw *string) *working_hours.Spec {
	spec, err := working_hours.DecodeSpec(raw)
	if err != nil {
		log.Printf("[workspace_config] workspace %s has an unreadable working-hours document; treating it as not configured: %v", workspaceID, err)
		return nil
	}
	return spec
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

		RouletteMode:                wsc.DefaultRouletteMode,
		RouletteLastSeenWindowHours: wsc.DefaultRouletteLastSeenWindowHours,
		RouletteRescueEnabled:       wsc.DefaultRouletteRescueEnabled,
		RouletteRescueAfterMinutes:  wsc.DefaultRouletteRescueAfterMinutes,
	}
	return r.db.WithContext(ctx).Omit("UpdatedBy").Create(row).Error
}

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

func (r *Repository) ListRoulettePolicies(ctx context.Context) ([]wsc.RoulettePolicy, error) {
	type row struct {
		WorkspaceID  string  `gorm:"column:workspace_id"`
		WindowHours  int     `gorm:"column:roulette_last_seen_window_hours"`
		AfterMinute  int     `gorm:"column:roulette_rescue_after_minutes"`
		WorkingHours *string `gorm:"column:working_hours"`
	}
	var rows []row
	err := r.db.WithContext(ctx).
		Model(&schema.WorkspaceConfig{}).
		Select("workspace_id, roulette_last_seen_window_hours, roulette_rescue_after_minutes, working_hours").
		Where("roulette_mode = ? AND roulette_rescue_enabled = ?", wsc.RouletteModeLastSeen, true).
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	out := make([]wsc.RoulettePolicy, 0, len(rows))
	for _, item := range rows {
		out = append(out, wsc.RoulettePolicy{
			WorkspaceID:    item.WorkspaceID,
			RescueAfter:    time.Duration(wsc.ClampRouletteRescueMinutes(item.AfterMinute)) * time.Minute,
			LastSeenWindow: time.Duration(wsc.ClampRouletteLastSeenWindowHours(item.WindowHours)) * time.Hour,
			WorkingHours:   decodeWorkingHours(item.WorkspaceID, item.WorkingHours),
		})
	}
	return out, nil
}
