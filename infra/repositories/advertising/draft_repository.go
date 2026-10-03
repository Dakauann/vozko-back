package advertising_repository

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"vozko/domain/advertising"
	"vozko/infra/database/schema"
)

type draftRepository struct {
	db *gorm.DB
}

func NewDraftRepository(db *gorm.DB) advertising.SavedDraftRepository {
	return &draftRepository{db: db}
}

func (r *draftRepository) Create(ctx context.Context, d *advertising.SavedDraft) error {
	if blank(d.WorkspaceID) {
		return advertising.ErrWorkspaceRequired
	}
	if blank(d.AdAccountID) {
		return errAccountRequired
	}
	content, err := encodeJSON(d.Content)
	if err != nil {
		return err
	}
	record := &schema.AdDraft{
		WorkspaceID: d.WorkspaceID, AdAccountID: d.AdAccountID, CreatedBy: d.CreatedBy, UpdatedBy: d.UpdatedBy,
		Content: content, Version: 1,
	}
	if err := r.db.WithContext(ctx).Create(record).Error; err != nil {
		return err
	}
	d.ID, d.Version, d.CreatedAt, d.UpdatedAt = record.ID, record.Version, record.CreatedAt, record.UpdatedAt
	return nil
}

func (r *draftRepository) Find(ctx context.Context, workspaceID, id string) (*advertising.SavedDraft, error) {
	if blank(workspaceID) {
		return nil, advertising.ErrWorkspaceRequired
	}
	var record schema.AdDraft
	if err := r.db.WithContext(ctx).First(&record, "workspace_id = ? AND id = ?", workspaceID, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, advertising.ErrDraftNotFound
		}
		return nil, err
	}
	return toDraft(&record)
}

func (r *draftRepository) Save(ctx context.Context, d *advertising.SavedDraft) error {
	if blank(d.WorkspaceID) {
		return advertising.ErrWorkspaceRequired
	}
	content, err := encodeJSON(d.Content)
	if err != nil {
		return err
	}
	result := r.db.WithContext(ctx).Model(&schema.AdDraft{}).
		Where("id = ? AND workspace_id = ? AND version = ?", d.ID, d.WorkspaceID, d.Version).
		Updates(map[string]any{
			"ad_account_id": d.AdAccountID,
			"updated_by":    d.UpdatedBy,
			"content":       content,
			"job_id":        nil,
			"version":       gorm.Expr("version + 1"),
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return r.missingOrChanged(ctx, d.WorkspaceID, d.ID)
	}
	d.Version++
	d.JobID = ""
	return nil
}

func (r *draftRepository) missingOrChanged(ctx context.Context, workspaceID, id string) error {
	var count int64
	if err := r.db.WithContext(ctx).Model(&schema.AdDraft{}).Where("id = ? AND workspace_id = ?", id, workspaceID).Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return advertising.ErrDraftNotFound
	}
	return advertising.ErrDraftChanged
}

func (r *draftRepository) ClaimForPublish(ctx context.Context, d *advertising.SavedDraft, jobID string) error {
	if blank(d.WorkspaceID) {
		return advertising.ErrWorkspaceRequired
	}
	result := r.db.WithContext(ctx).Model(&schema.AdDraft{}).
		Where("id = ? AND workspace_id = ? AND version = ?", d.ID, d.WorkspaceID, d.Version).
		Updates(map[string]any{"job_id": jobID, "version": gorm.Expr("version + 1")})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return r.missingOrChanged(ctx, d.WorkspaceID, d.ID)
	}
	d.Version++
	d.JobID = jobID
	return nil
}

func (r *draftRepository) ReleaseJob(ctx context.Context, workspaceID, id, jobID string) error {
	if blank(workspaceID) {
		return advertising.ErrWorkspaceRequired
	}
	return r.db.WithContext(ctx).Model(&schema.AdDraft{}).
		Where("id = ? AND workspace_id = ? AND job_id = ?", id, workspaceID, jobID).
		UpdateColumn("job_id", nil).Error
}

func (r *draftRepository) Delete(ctx context.Context, d *advertising.SavedDraft) error {
	if blank(d.WorkspaceID) {
		return advertising.ErrWorkspaceRequired
	}
	result := r.db.WithContext(ctx).Where("id = ? AND workspace_id = ? AND version = ?", d.ID, d.WorkspaceID, d.Version).Delete(&schema.AdDraft{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return r.missingOrChanged(ctx, d.WorkspaceID, d.ID)
	}
	return nil
}

func (r *draftRepository) ListByAccount(ctx context.Context, workspaceID, accountID string) ([]*advertising.SavedDraft, error) {
	if blank(workspaceID) {
		return nil, advertising.ErrWorkspaceRequired
	}
	var records []schema.AdDraft
	if err := r.db.WithContext(ctx).Where("workspace_id = ? AND ad_account_id = ?", workspaceID, accountID).
		Order("updated_at DESC, id").Find(&records).Error; err != nil {
		return nil, err
	}
	out := make([]*advertising.SavedDraft, 0, len(records))
	for i := range records {
		d, err := toDraft(&records[i])
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, nil
}

func toDraft(r *schema.AdDraft) (*advertising.SavedDraft, error) {
	d := &advertising.SavedDraft{
		ID: r.ID, WorkspaceID: r.WorkspaceID, AdAccountID: r.AdAccountID, CreatedBy: r.CreatedBy, UpdatedBy: r.UpdatedBy,
		Version: r.Version, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
	if r.JobID != nil {
		d.JobID = *r.JobID
	}
	if err := decodeJSON(r.Content, &d.Content); err != nil {
		return nil, err
	}
	return d, nil
}
