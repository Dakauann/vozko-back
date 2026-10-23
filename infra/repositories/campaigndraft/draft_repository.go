package campaigndraft_repository

import (
	"context"
	"encoding/json"
	"errors"

	"gorm.io/datatypes"
	"gorm.io/gorm"

	"vozko/domain/campaigndraft"
	"vozko/infra/database/schema"
)

type repository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) campaigndraft.Repository {
	return &repository{db: db}
}

func rowOf(d *campaigndraft.Draft) *schema.CampaignDraft {
	return &schema.CampaignDraft{
		ID: d.ID, WorkspaceID: d.WorkspaceID, Channel: string(d.Channel), Name: d.Name,
		Payload: datatypes.JSON(d.Payload), CreatedBy: d.CreatedBy, CreatedAt: d.CreatedAt.UTC(), UpdatedAt: d.UpdatedAt.UTC(),
	}
}

func draftOf(row schema.CampaignDraft) campaigndraft.Draft {
	return campaigndraft.Draft{
		ID: row.ID, WorkspaceID: row.WorkspaceID, Channel: campaigndraft.Channel(row.Channel), Name: row.Name,
		Payload: json.RawMessage(row.Payload), CreatedBy: row.CreatedBy, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
}

func (r *repository) Create(ctx context.Context, d *campaigndraft.Draft) error {
	return r.db.WithContext(ctx).Create(rowOf(d)).Error
}

func (r *repository) Update(ctx context.Context, d *campaigndraft.Draft) error {
	result := r.db.WithContext(ctx).Model(&schema.CampaignDraft{}).
		Where("workspace_id = ? AND channel = ? AND id = ?", d.WorkspaceID, string(d.Channel), d.ID).
		Updates(map[string]any{"name": d.Name, "payload": datatypes.JSON(d.Payload), "updated_at": d.UpdatedAt.UTC()})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return campaigndraft.ErrNotFound
	}
	return nil
}

func (r *repository) Find(ctx context.Context, workspaceID string, ch campaigndraft.Channel, id string) (*campaigndraft.Draft, error) {
	var row schema.CampaignDraft
	err := r.db.WithContext(ctx).Where("workspace_id = ? AND channel = ? AND id = ?", workspaceID, string(ch), id).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, campaigndraft.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	d := draftOf(row)
	return &d, nil
}

func (r *repository) List(ctx context.Context, workspaceID string, ch campaigndraft.Channel, limit int) ([]campaigndraft.Draft, error) {
	var rows []schema.CampaignDraft
	err := r.db.WithContext(ctx).Where("workspace_id = ? AND channel = ?", workspaceID, string(ch)).
		Order("updated_at DESC, id DESC").Limit(limit).Find(&rows).Error
	if err != nil {
		return nil, err
	}
	drafts := make([]campaigndraft.Draft, 0, len(rows))
	for _, row := range rows {
		drafts = append(drafts, draftOf(row))
	}
	return drafts, nil
}

func (r *repository) Delete(ctx context.Context, workspaceID string, ch campaigndraft.Channel, id string) error {
	result := r.db.WithContext(ctx).Where("workspace_id = ? AND channel = ? AND id = ?", workspaceID, string(ch), id).Delete(&schema.CampaignDraft{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return campaigndraft.ErrNotFound
	}
	return nil
}
