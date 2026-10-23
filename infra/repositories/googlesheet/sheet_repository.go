package googlesheet_repository

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"gorm.io/gorm"

	"vozko/domain/googlesheet"
	"vozko/domain/shared"
	"vozko/infra/database/schema"
)

type sheetRepository struct {
	db *gorm.DB
}

func NewSheetRepository(db *gorm.DB) googlesheet.SheetRepository {
	return &sheetRepository{db: db}
}

func (r *sheetRepository) Create(ctx context.Context, s *googlesheet.Sheet) error {
	record, err := toSheetSchema(s)
	if err != nil {
		return err
	}
	if err := r.db.WithContext(ctx).Create(record).Error; err != nil {
		return err
	}
	s.ID, s.CreatedAt, s.UpdatedAt = record.ID, record.CreatedAt, record.UpdatedAt
	return nil
}

func (r *sheetRepository) Update(ctx context.Context, s *googlesheet.Sheet, expectedVersion int) error {
	columns, err := json.Marshal(s.Columns)
	if err != nil {
		return err
	}
	result := r.db.WithContext(ctx).Model(&schema.GoogleSheet{}).
		Where("id = ? AND workspace_id = ? AND version = ?", s.ID, s.WorkspaceID, expectedVersion).
		Updates(map[string]any{
			"title":         s.Title,
			"description":   s.Description,
			"columns":       columns,
			"status":        string(s.Status),
			"reason":        string(s.Reason),
			"reason_detail": clip(s.ReasonDetail, 500),
			"version":       s.Version,
			"updated_at":    time.Now().UTC(),
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return googlesheet.ErrStaleSheet
	}
	return nil
}

func (r *sheetRepository) SaveStatus(ctx context.Context, s *googlesheet.Sheet) error {
	result := r.db.WithContext(ctx).Model(&schema.GoogleSheet{}).Where("id = ?", s.ID).Updates(map[string]any{
		"status":        string(s.Status),
		"reason":        string(s.Reason),
		"reason_detail": clip(s.ReasonDetail, 500),
		"updated_at":    time.Now().UTC(),
	})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return googlesheet.ErrSheetNotFound
	}
	return nil
}

func (r *sheetRepository) UpdateTabName(ctx context.Context, id, tabName string) error {
	return r.db.WithContext(ctx).Model(&schema.GoogleSheet{}).Where("id = ?", id).
		Updates(map[string]any{"tab_name": tabName, "updated_at": time.Now().UTC()}).Error
}

func (r *sheetRepository) Get(ctx context.Context, id string) (*googlesheet.Sheet, error) {
	return r.find(r.db.WithContext(ctx).Where("id = ?", id))
}

func (r *sheetRepository) FindInWorkspace(ctx context.Context, workspaceID, id string) (*googlesheet.Sheet, error) {
	return r.find(r.db.WithContext(ctx).Where("id = ? AND workspace_id = ?", id, workspaceID))
}

func (r *sheetRepository) find(query *gorm.DB) (*googlesheet.Sheet, error) {
	var record schema.GoogleSheet
	if err := query.Limit(1).Find(&record).Error; err != nil {
		return nil, err
	}
	if record.ID == "" {
		return nil, googlesheet.ErrSheetNotFound
	}
	return toSheetDomain(&record)
}

func (r *sheetRepository) List(ctx context.Context, in googlesheet.ListInput) (*shared.PaginatedResult[*googlesheet.Sheet], error) {
	pagination := shared.NormalizePagination(in.Pagination)
	query := r.db.WithContext(ctx).Model(&schema.GoogleSheet{}).Where("workspace_id = ?", in.WorkspaceID)
	if search := strings.TrimSpace(in.Search); search != "" {
		query = query.Where("title ILIKE ?", "%"+escapeLike(search)+"%")
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, err
	}
	var records []schema.GoogleSheet
	if err := query.Order("created_at DESC").Order("id DESC").Offset(pagination.Offset()).Limit(pagination.PageSize).Find(&records).Error; err != nil {
		return nil, err
	}
	sheets := make([]*googlesheet.Sheet, 0, len(records))
	for i := range records {
		s, err := toSheetDomain(&records[i])
		if err != nil {
			return nil, err
		}
		sheets = append(sheets, s)
	}
	return shared.NewPaginatedResult(sheets, pagination, total), nil
}

func (r *sheetRepository) Delete(ctx context.Context, workspaceID, id string) error {
	result := r.db.WithContext(ctx).Where("id = ? AND workspace_id = ?", id, workspaceID).Delete(&schema.GoogleSheet{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return googlesheet.ErrSheetNotFound
	}
	return nil
}

func (r *sheetRepository) TouchWritten(ctx context.Context, id string, at time.Time) error {
	return r.db.WithContext(ctx).Model(&schema.GoogleSheet{}).Where("id = ?", id).
		UpdateColumn("last_written_at", at).Error
}

func toSheetSchema(s *googlesheet.Sheet) (*schema.GoogleSheet, error) {
	columns, err := json.Marshal(s.Columns)
	if err != nil {
		return nil, err
	}
	record := &schema.GoogleSheet{
		ID:            s.ID,
		WorkspaceID:   s.WorkspaceID,
		AccountID:     s.AccountID,
		SpreadsheetID: s.SpreadsheetID,
		TabID:         s.TabID,
		TabName:       s.TabName,
		Title:         s.Title,
		Description:   s.Description,
		TimeZone:      s.TimeZone,
		Locale:        s.Locale,
		Columns:       columns,
		Status:        string(s.Status),
		Reason:        string(s.Reason),
		ReasonDetail:  clip(s.ReasonDetail, 500),
		Version:       s.Version,
	}
	if s.CreatedBy != "" {
		record.CreatedBy = &s.CreatedBy
	}
	return record, nil
}

func toSheetDomain(record *schema.GoogleSheet) (*googlesheet.Sheet, error) {
	var columns []googlesheet.Column
	if err := json.Unmarshal(record.Columns, &columns); err != nil {
		return nil, err
	}
	s := &googlesheet.Sheet{
		ID:            record.ID,
		WorkspaceID:   record.WorkspaceID,
		AccountID:     record.AccountID,
		SpreadsheetID: record.SpreadsheetID,
		TabID:         record.TabID,
		TabName:       record.TabName,
		Title:         record.Title,
		Description:   record.Description,
		TimeZone:      record.TimeZone,
		Locale:        record.Locale,
		Columns:       columns,
		Status:        googlesheet.Status(record.Status),
		Reason:        googlesheet.AttentionReason(record.Reason),
		ReasonDetail:  record.ReasonDetail,
		Version:       record.Version,
		CreatedAt:     record.CreatedAt,
		UpdatedAt:     record.UpdatedAt,
		LastWrittenAt: record.LastWrittenAt,
	}
	if record.CreatedBy != nil {
		s.CreatedBy = *record.CreatedBy
	}
	return s, nil
}

func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

func clip(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max])
}
