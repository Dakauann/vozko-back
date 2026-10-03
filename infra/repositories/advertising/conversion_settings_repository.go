package advertising_repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	"vozko/domain/advertising"
	"vozko/infra/database/schema"
)

type conversionSettingsRepository struct {
	db *gorm.DB
}

func NewConversionSettingsRepository(db *gorm.DB) advertising.ConversionSettingsRepository {
	return &conversionSettingsRepository{db: db}
}

const saveConversionSettingsSQL = `INSERT INTO ad_conversion_settings (workspace_id, ad_account_id, dataset_id, pixel_id, send_leads, send_purchases, enabled, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, NOW())
ON CONFLICT (workspace_id) DO UPDATE SET
	ad_account_id = EXCLUDED.ad_account_id,
	dataset_id = EXCLUDED.dataset_id,
	pixel_id = EXCLUDED.pixel_id,
	send_leads = EXCLUDED.send_leads,
	send_purchases = EXCLUDED.send_purchases,
	enabled = EXCLUDED.enabled,
	updated_at = NOW()
RETURNING updated_at`

func (r *conversionSettingsRepository) Get(ctx context.Context, workspaceID string) (*advertising.ConversionSettings, error) {
	if blank(workspaceID) {
		return nil, advertising.ErrWorkspaceRequired
	}
	var record schema.AdConversionSettings
	if err := r.db.WithContext(ctx).First(&record, "workspace_id = ?", workspaceID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, advertising.ErrSettingsNotFound
		}
		return nil, err
	}
	return toConversionSettings(&record), nil
}

func (r *conversionSettingsRepository) Save(ctx context.Context, s *advertising.ConversionSettings) error {
	if blank(s.WorkspaceID) {
		return advertising.ErrWorkspaceRequired
	}
	type saved struct {
		UpdatedAt time.Time `gorm:"column:updated_at"`
	}
	var rows []saved
	if err := r.db.WithContext(ctx).Raw(saveConversionSettingsSQL,
		s.WorkspaceID, s.AdAccountID, s.DatasetID, s.PixelID, s.SendLeads, s.SendPurchases, s.Enabled,
	).Scan(&rows).Error; err != nil {
		return err
	}
	if len(rows) != 1 {
		return errSettingsNotSaved
	}
	s.UpdatedAt = rows[0].UpdatedAt
	return nil
}

func (r *conversionSettingsRepository) ListEnabled(ctx context.Context) ([]*advertising.ConversionSettings, error) {
	var records []schema.AdConversionSettings
	if err := r.db.WithContext(ctx).Where("enabled = ?", true).Order("workspace_id").Find(&records).Error; err != nil {
		return nil, err
	}
	out := make([]*advertising.ConversionSettings, 0, len(records))
	for i := range records {
		out = append(out, toConversionSettings(&records[i]))
	}
	return out, nil
}

func toConversionSettings(record *schema.AdConversionSettings) *advertising.ConversionSettings {
	return &advertising.ConversionSettings{
		WorkspaceID:   record.WorkspaceID,
		AdAccountID:   record.AdAccountID,
		DatasetID:     record.DatasetID,
		PixelID:       record.PixelID,
		SendLeads:     record.SendLeads,
		SendPurchases: record.SendPurchases,
		Enabled:       record.Enabled,
		UpdatedAt:     record.UpdatedAt,
	}
}
