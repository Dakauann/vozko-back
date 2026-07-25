package shortlink

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	"vozko/domain/shared"
	"vozko/domain/shortlink"
	"vozko/infra/database/schema"
)

type shortLinkRepository struct {
	db *gorm.DB
}

func NewShortLinkRepository(db *gorm.DB) shortlink.ShortLinkRepository {
	return &shortLinkRepository{db: db}
}

func (r *shortLinkRepository) Create(ctx context.Context, link *shortlink.ShortLink) error {
	return r.db.WithContext(ctx).Create(mapShortLinkToSchema(link)).Error
}

func (r *shortLinkRepository) Update(ctx context.Context, link *shortlink.ShortLink) error {
	update := map[string]any{
		"target_url":    link.TargetURL,
		"title":         link.Title,
		"redirect_type": string(link.RedirectType),
		"status":        string(link.Status),
		"password_hash": link.PasswordHash,
		"expires_at":    link.ExpiresAt,
		"max_clicks":    link.MaxClicks,
		"updated_at":    link.UpdatedAt,
	}
	result := r.db.WithContext(ctx).
		Model(&schema.ShortLink{}).
		Where("id = ? AND workspace_id = ?", link.ID, link.WorkspaceID).
		Updates(update)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return shortlink.ErrShortLinkNotFound
	}
	return nil
}

func (r *shortLinkRepository) SoftDelete(ctx context.Context, workspaceID, id string) error {
	result := r.db.WithContext(ctx).
		Where("id = ? AND workspace_id = ?", id, workspaceID).
		Delete(&schema.ShortLink{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return shortlink.ErrShortLinkNotFound
	}
	return nil
}

func (r *shortLinkRepository) FindByID(ctx context.Context, workspaceID, id string) (*shortlink.ShortLink, error) {
	var row schema.ShortLink
	if err := r.db.WithContext(ctx).Where("id = ? AND workspace_id = ?", id, workspaceID).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, shortlink.ErrShortLinkNotFound
		}
		return nil, err
	}
	return mapShortLinkToDomain(&row), nil
}

func (r *shortLinkRepository) FindByCode(ctx context.Context, code string) (*shortlink.ShortLink, error) {
	var row schema.ShortLink
	if err := r.db.WithContext(ctx).Where("code = ?", code).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, shortlink.ErrShortLinkNotFound
		}
		return nil, err
	}
	return mapShortLinkToDomain(&row), nil
}

func (r *shortLinkRepository) CodeExists(ctx context.Context, code string) (bool, error) {
	var count int64
	if err := r.db.WithContext(ctx).Model(&schema.ShortLink{}).Where("code = ?", code).Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func (r *shortLinkRepository) ListByWorkspace(ctx context.Context, workspaceID string, departmentID *string, opts shared.Pagination) (*shared.PaginatedResult[*shortlink.ShortLink], error) {
	pagination := shared.NormalizePagination(opts)

	base := r.db.WithContext(ctx).Model(&schema.ShortLink{}).Where("workspace_id = ?", workspaceID)
	if departmentID != nil {
		base = base.Where("department_id = ?", *departmentID)
	}

	var total int64
	if err := base.Count(&total).Error; err != nil {
		return nil, err
	}

	var rows []schema.ShortLink
	query := r.db.WithContext(ctx).Where("workspace_id = ?", workspaceID)
	if departmentID != nil {
		query = query.Where("department_id = ?", *departmentID)
	}
	if err := query.
		Order("created_at DESC").
		Offset(pagination.Offset()).
		Limit(pagination.PageSize).
		Find(&rows).Error; err != nil {
		return nil, err
	}

	items := make([]*shortlink.ShortLink, 0, len(rows))
	for i := range rows {
		items = append(items, mapShortLinkToDomain(&rows[i]))
	}
	return shared.NewPaginatedResult(items, pagination, total), nil
}

func (r *shortLinkRepository) CountByWorkspace(ctx context.Context, workspaceID string) (int, error) {
	var count int64
	if err := r.db.WithContext(ctx).Model(&schema.ShortLink{}).Where("workspace_id = ?", workspaceID).Count(&count).Error; err != nil {
		return 0, err
	}
	return int(count), nil
}

func (r *shortLinkRepository) SumClicksByWorkspace(ctx context.Context, workspaceID string) (int64, error) {
	var sum int64
	if err := r.db.WithContext(ctx).
		Model(&schema.ShortLink{}).
		Where("workspace_id = ?", workspaceID).
		Select("COALESCE(SUM(click_count), 0)").
		Scan(&sum).Error; err != nil {
		return 0, err
	}
	return sum, nil
}

func (r *shortLinkRepository) ApplyClick(ctx context.Context, id string, uniqueDelta int64, occurredAt time.Time) error {
	return r.db.WithContext(ctx).
		Model(&schema.ShortLink{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"click_count":        gorm.Expr("click_count + 1"),
			"unique_click_count": gorm.Expr("unique_click_count + ?", uniqueDelta),
			"last_clicked_at":    occurredAt,
		}).Error
}

func mapShortLinkToSchema(link *shortlink.ShortLink) *schema.ShortLink {
	row := &schema.ShortLink{
		ID:               link.ID,
		WorkspaceID:      link.WorkspaceID,
		Code:             link.Code,
		TargetURL:        link.TargetURL,
		Title:            link.Title,
		RedirectType:     string(link.RedirectType),
		Status:           string(link.Status),
		PasswordHash:     link.PasswordHash,
		ExpiresAt:        link.ExpiresAt,
		MaxClicks:        link.MaxClicks,
		ClickCount:       link.ClickCount,
		UniqueClickCount: link.UniqueClickCount,
		LastClickedAt:    link.LastClickedAt,
		CreatedAt:        link.CreatedAt,
		UpdatedAt:        link.UpdatedAt,
	}
	if link.DepartmentID != "" {
		dept := link.DepartmentID
		row.DepartmentID = &dept
	}
	if link.CreatedBy != "" {
		createdBy := link.CreatedBy
		row.CreatedBy = &createdBy
	}
	return row
}

func mapShortLinkToDomain(row *schema.ShortLink) *shortlink.ShortLink {
	link := &shortlink.ShortLink{
		ID:               row.ID,
		WorkspaceID:      row.WorkspaceID,
		Code:             row.Code,
		TargetURL:        row.TargetURL,
		Title:            row.Title,
		RedirectType:     shortlink.RedirectType(row.RedirectType),
		Status:           shortlink.LinkStatus(row.Status),
		PasswordHash:     row.PasswordHash,
		HasPassword:      row.PasswordHash != "",
		ExpiresAt:        row.ExpiresAt,
		MaxClicks:        row.MaxClicks,
		ClickCount:       row.ClickCount,
		UniqueClickCount: row.UniqueClickCount,
		LastClickedAt:    row.LastClickedAt,
		CreatedAt:        row.CreatedAt,
		UpdatedAt:        row.UpdatedAt,
	}
	if row.DepartmentID != nil {
		link.DepartmentID = *row.DepartmentID
	}
	if row.CreatedBy != nil {
		link.CreatedBy = *row.CreatedBy
	}
	return link
}
