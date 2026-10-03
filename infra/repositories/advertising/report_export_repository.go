package advertising_repository

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"vozko/domain/advertising"
	"vozko/infra/database/schema"
)

type reportExportRepository struct {
	db *gorm.DB
}

func NewReportExportRepository(db *gorm.DB) advertising.ReportExportRepository {
	return &reportExportRepository{db: db}
}

const reportExportSummaryColumns = "id, workspace_id, ad_account_id, report_id, name, since, until, rows, size_bytes, created_by, created_at"

func (r *reportExportRepository) Create(ctx context.Context, e *advertising.ReportExport) error {
	if blank(e.WorkspaceID) {
		return advertising.ErrWorkspaceRequired
	}
	if blank(e.AdAccountID) {
		return errAccountRequired
	}
	record := &schema.AdReportExport{
		WorkspaceID: e.WorkspaceID, AdAccountID: e.AdAccountID, Name: e.Name, Since: e.Range.Since, Until: e.Range.Until,
		Rows: e.Rows, SizeBytes: e.SizeBytes, Content: e.Content, CreatedBy: e.CreatedBy,
	}
	if e.ReportID != "" {
		record.ReportID = &e.ReportID
	}
	if err := r.db.WithContext(ctx).Create(record).Error; err != nil {
		return err
	}
	e.ID, e.CreatedAt = record.ID, record.CreatedAt
	return nil
}

func (r *reportExportRepository) Find(ctx context.Context, workspaceID, id string) (*advertising.ReportExport, error) {
	if blank(workspaceID) {
		return nil, advertising.ErrWorkspaceRequired
	}
	var record schema.AdReportExport
	if err := r.db.WithContext(ctx).First(&record, "workspace_id = ? AND id = ?", workspaceID, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, advertising.ErrReportExportNotFound
		}
		return nil, err
	}
	return toReportExport(&record), nil
}

func (r *reportExportRepository) List(ctx context.Context, workspaceID string, limit int) ([]*advertising.ReportExport, error) {
	if blank(workspaceID) {
		return nil, advertising.ErrWorkspaceRequired
	}
	var records []schema.AdReportExport
	if err := r.db.WithContext(ctx).Select(reportExportSummaryColumns).Where("workspace_id = ?", workspaceID).
		Order("created_at DESC, id").Limit(limit).Find(&records).Error; err != nil {
		return nil, err
	}
	out := make([]*advertising.ReportExport, 0, len(records))
	for i := range records {
		out = append(out, toReportExport(&records[i]))
	}
	return out, nil
}

func (r *reportExportRepository) Delete(ctx context.Context, workspaceID, id string) error {
	if blank(workspaceID) {
		return advertising.ErrWorkspaceRequired
	}
	result := r.db.WithContext(ctx).Where("id = ? AND workspace_id = ?", id, workspaceID).Delete(&schema.AdReportExport{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return advertising.ErrReportExportNotFound
	}
	return nil
}

func (r *reportExportRepository) KeepNewest(ctx context.Context, workspaceID string, keep int) error {
	if blank(workspaceID) {
		return advertising.ErrWorkspaceRequired
	}
	newest := r.db.Model(&schema.AdReportExport{}).Select("id").Where("workspace_id = ?", workspaceID).Order("created_at DESC, id").Limit(keep)
	return r.db.WithContext(ctx).Where("workspace_id = ? AND id NOT IN (?)", workspaceID, newest).Delete(&schema.AdReportExport{}).Error
}

func toReportExport(r *schema.AdReportExport) *advertising.ReportExport {
	e := &advertising.ReportExport{
		ID: r.ID, WorkspaceID: r.WorkspaceID, AdAccountID: r.AdAccountID, Name: r.Name,
		Range: advertising.DateRange{Since: r.Since, Until: r.Until}, Rows: r.Rows, SizeBytes: r.SizeBytes, Content: r.Content,
		CreatedBy: r.CreatedBy, CreatedAt: r.CreatedAt,
	}
	if r.ReportID != nil {
		e.ReportID = *r.ReportID
	}
	return e
}
