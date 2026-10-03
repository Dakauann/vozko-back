package advertising_repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	"vozko/domain/advertising"
	"vozko/infra/database/schema"
)

type savedReportRepository struct {
	db *gorm.DB
}

func NewSavedReportRepository(db *gorm.DB) advertising.SavedReportRepository {
	return &savedReportRepository{db: db}
}

func (r *savedReportRepository) Create(ctx context.Context, report *advertising.SavedReport) error {
	if blank(report.WorkspaceID) {
		return advertising.ErrWorkspaceRequired
	}
	if blank(report.AdAccountID) {
		return errAccountRequired
	}
	definition, err := encodeJSON(report.Definition)
	if err != nil {
		return err
	}
	record := &schema.AdSavedReport{
		WorkspaceID: report.WorkspaceID, AdAccountID: report.AdAccountID, Name: report.Name, Definition: definition,
		CreatedBy: report.CreatedBy, LastOpenedAt: report.LastOpenedAt,
	}
	if err := r.db.WithContext(ctx).Create(record).Error; err != nil {
		return err
	}
	report.ID, report.CreatedAt, report.UpdatedAt = record.ID, record.CreatedAt, record.UpdatedAt
	return nil
}

func (r *savedReportRepository) Find(ctx context.Context, workspaceID, id string) (*advertising.SavedReport, error) {
	if blank(workspaceID) {
		return nil, advertising.ErrWorkspaceRequired
	}
	var record schema.AdSavedReport
	if err := r.db.WithContext(ctx).First(&record, "workspace_id = ? AND id = ?", workspaceID, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, advertising.ErrReportNotFound
		}
		return nil, err
	}
	return toSavedReport(&record)
}

func (r *savedReportRepository) Save(ctx context.Context, report *advertising.SavedReport) error {
	if blank(report.WorkspaceID) {
		return advertising.ErrWorkspaceRequired
	}
	definition, err := encodeJSON(report.Definition)
	if err != nil {
		return err
	}
	return r.update(ctx, report.WorkspaceID, report.ID, map[string]any{
		"ad_account_id": report.AdAccountID, "name": report.Name, "definition": definition,
	})
}

func (r *savedReportRepository) MarkOpened(ctx context.Context, workspaceID, id string, at time.Time) error {
	if blank(workspaceID) {
		return advertising.ErrWorkspaceRequired
	}
	result := r.db.WithContext(ctx).Model(&schema.AdSavedReport{}).Where("id = ? AND workspace_id = ?", id, workspaceID).UpdateColumn("last_opened_at", at)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return advertising.ErrReportNotFound
	}
	return nil
}

func (r *savedReportRepository) update(ctx context.Context, workspaceID, id string, fields map[string]any) error {
	result := r.db.WithContext(ctx).Model(&schema.AdSavedReport{}).Where("id = ? AND workspace_id = ?", id, workspaceID).Updates(fields)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return advertising.ErrReportNotFound
	}
	return nil
}

func (r *savedReportRepository) Delete(ctx context.Context, workspaceID, id string) error {
	if blank(workspaceID) {
		return advertising.ErrWorkspaceRequired
	}
	result := r.db.WithContext(ctx).Where("id = ? AND workspace_id = ?", id, workspaceID).Delete(&schema.AdSavedReport{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return advertising.ErrReportNotFound
	}
	return nil
}

func (r *savedReportRepository) ListByWorkspace(ctx context.Context, workspaceID string) ([]*advertising.SavedReport, error) {
	if blank(workspaceID) {
		return nil, advertising.ErrWorkspaceRequired
	}
	var records []schema.AdSavedReport
	if err := r.db.WithContext(ctx).Where("workspace_id = ?", workspaceID).
		Order("last_opened_at DESC NULLS LAST, updated_at DESC, id").Find(&records).Error; err != nil {
		return nil, err
	}
	out := make([]*advertising.SavedReport, 0, len(records))
	for i := range records {
		report, err := toSavedReport(&records[i])
		if err != nil {
			return nil, err
		}
		out = append(out, report)
	}
	return out, nil
}

func toSavedReport(r *schema.AdSavedReport) (*advertising.SavedReport, error) {
	report := &advertising.SavedReport{
		ID: r.ID, WorkspaceID: r.WorkspaceID, AdAccountID: r.AdAccountID, Name: r.Name, CreatedBy: r.CreatedBy,
		LastOpenedAt: r.LastOpenedAt, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
	if err := decodeJSON(r.Definition, &report.Definition); err != nil {
		return nil, err
	}
	return report, nil
}
