package config_repository

import (
	"context"
	"errors"
	"time"

	"vozko/domain/config"
	"vozko/infra/database/schema"

	"gorm.io/gorm"
)

type SystemConfigRepository struct {
	db *gorm.DB
}

func NewSystemConfigRepository(db *gorm.DB) config.SystemConfigRepository {
	return &SystemConfigRepository{db: db}
}

func (r *SystemConfigRepository) Get(ctx context.Context) (*config.SystemConfig, error) {
	var dbConfig schema.SystemConfig
	if err := r.db.WithContext(ctx).Where("id = ?", "default").First(&dbConfig).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return &config.SystemConfig{
				ID:                     "default",
				BaseSystemPrompt:       "",
				MaxConcurrentCalls:     config.DefaultMaxConcurrentCalls,
				WorkTimeEnabled:        false,
				WorkTimeStart:          config.DefaultWorkTimeStart,
				WorkTimeEnd:            config.DefaultWorkTimeEnd,
				AffiliateCommissionPct: config.DefaultAffiliateCommissionPct,
			}, nil
		}
		return nil, err
	}
	cfg := &config.SystemConfig{
		ID:                     dbConfig.ID,
		BaseSystemPrompt:       dbConfig.BaseSystemPrompt,
		MaxConcurrentCalls:     dbConfig.MaxConcurrentCalls,
		WorkTimeEnabled:        dbConfig.WorkTimeEnabled,
		WorkTimeStart:          dbConfig.WorkTimeStart,
		WorkTimeEnd:            dbConfig.WorkTimeEnd,
		AffiliateCommissionPct: dbConfig.AffiliateCommissionPct,
		UpdatedBy:              dbConfig.UpdatedBy,
		UpdatedAt:              dbConfig.UpdatedAt.Format(time.RFC3339),
	}
	cfg.MaxConcurrentCalls = cfg.GetMaxConcurrentCalls()
	return cfg, nil
}

func (r *SystemConfigRepository) Upsert(ctx context.Context, cfg *config.SystemConfig) error {
	dbConfig := &schema.SystemConfig{
		ID:                     "default",
		BaseSystemPrompt:       cfg.BaseSystemPrompt,
		MaxConcurrentCalls:     cfg.MaxConcurrentCalls,
		WorkTimeEnabled:        cfg.WorkTimeEnabled,
		WorkTimeStart:          cfg.WorkTimeStart,
		WorkTimeEnd:            cfg.WorkTimeEnd,
		AffiliateCommissionPct: cfg.AffiliateCommissionPct,
		UpdatedBy:              cfg.UpdatedBy,
	}

	return r.db.WithContext(ctx).Save(dbConfig).Error
}
