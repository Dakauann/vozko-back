package googlesheet_repository

import (
	"context"
	"vozko/domain/googlesheet"
	"vozko/domain/shared"
	"vozko/infra/database/schema"
)

func (r *rowRepository) ListRows(ctx context.Context, workspaceID, sheetID string, p shared.Pagination) (*shared.PaginatedResult[*googlesheet.Row], error) {
	query := r.db.WithContext(ctx).Model(&schema.GoogleSheetRow{}).Where("workspace_id = ? AND sheet_id = ?", workspaceID, sheetID)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, err
	}
	var records []schema.GoogleSheetRow
	if err := query.Order("created_at DESC, id DESC").Offset((p.Page - 1) * p.PageSize).Limit(p.PageSize).Find(&records).Error; err != nil {
		return nil, err
	}
	rows, err := toRowDomains(records)
	if err != nil {
		return nil, err
	}
	return shared.NewPaginatedResult(rows, p, total), nil
}
