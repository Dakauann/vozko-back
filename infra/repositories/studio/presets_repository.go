package studio_repository

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"vozko/domain/studio"
	"vozko/infra/database/schema"
)

type presetRepository struct {
	db *gorm.DB
}

var _ studio.PresetRepository = (*presetRepository)(nil)

func NewPresets(db *gorm.DB) studio.PresetRepository {
	return &presetRepository{db: db}
}

func (r *presetRepository) scoped(ctx context.Context, workspaceID string) *gorm.DB {
	return r.db.WithContext(ctx).Model(&schema.StudioAnimationPreset{}).Where("workspace_id = ?", workspaceID)
}

func (r *presetRepository) CreatePreset(ctx context.Context, p *studio.AnimationPreset) error {
	if !validID(p.WorkspaceID) {
		return studio.ErrWorkspaceRequired
	}
	if !validID(p.CreatedBy) {
		return studio.ErrCreatorRequired
	}
	record := &schema.StudioAnimationPreset{
		WorkspaceID: p.WorkspaceID, Name: p.Name, Description: p.Description, Animation: []byte(p.Animation), CreatedBy: p.CreatedBy,
	}
	if err := r.db.WithContext(ctx).Create(record).Error; err != nil {
		return err
	}
	p.ID, p.CreatedAt, p.UpdatedAt = record.ID, record.CreatedAt, record.UpdatedAt
	return nil
}

func (r *presetRepository) ListPresets(ctx context.Context, workspaceID string) ([]studio.AnimationPreset, error) {
	if !validID(workspaceID) {
		return nil, studio.ErrWorkspaceRequired
	}
	var records []schema.StudioAnimationPreset
	if err := r.scoped(ctx, workspaceID).Order("created_at DESC").Order("id DESC").Find(&records).Error; err != nil {
		return nil, err
	}
	out := make([]studio.AnimationPreset, 0, len(records))
	for i := range records {
		out = append(out, *presetToDomain(&records[i]))
	}
	return out, nil
}

func (r *presetRepository) GetPreset(ctx context.Context, workspaceID, id string) (*studio.AnimationPreset, error) {
	if !validID(id) || !validID(workspaceID) {
		return nil, studio.ErrPresetNotFound
	}
	var record schema.StudioAnimationPreset
	err := r.scoped(ctx, workspaceID).Where("id = ?", id).First(&record).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, studio.ErrPresetNotFound
	}
	if err != nil {
		return nil, err
	}
	return presetToDomain(&record), nil
}

func (r *presetRepository) SavePreset(ctx context.Context, p *studio.AnimationPreset) error {
	if !validID(p.ID) || !validID(p.WorkspaceID) {
		return studio.ErrPresetNotFound
	}
	var saved []schema.StudioAnimationPreset
	err := r.db.WithContext(ctx).Raw(`UPDATE studio_animation_presets SET name = ?, description = ?, updated_at = NOW()
		WHERE id = ? AND workspace_id = ? RETURNING *`, p.Name, p.Description, p.ID, p.WorkspaceID).Scan(&saved).Error
	if err != nil {
		return err
	}
	if len(saved) != 1 {
		return studio.ErrPresetNotFound
	}
	p.UpdatedAt = saved[0].UpdatedAt
	return nil
}

func (r *presetRepository) DeletePreset(ctx context.Context, workspaceID, id string) error {
	if !validID(id) || !validID(workspaceID) {
		return studio.ErrPresetNotFound
	}
	result := r.db.WithContext(ctx).Where("workspace_id = ? AND id = ?", workspaceID, id).Delete(&schema.StudioAnimationPreset{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return studio.ErrPresetNotFound
	}
	return nil
}

func presetToDomain(r *schema.StudioAnimationPreset) *studio.AnimationPreset {
	return &studio.AnimationPreset{
		ID: r.ID, WorkspaceID: r.WorkspaceID, Name: r.Name, Description: r.Description, Animation: []byte(r.Animation),
		CreatedBy: r.CreatedBy, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
}
