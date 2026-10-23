package carriercampaign_repository

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"

	"vozko/domain/carriercampaign"
	"vozko/domain/shared"
	"vozko/infra/database/schema"
	"vozko/infra/database/sqlexpr"
)

type campaignRepository struct {
	db *gorm.DB
}

func NewCampaignRepository(db *gorm.DB) carriercampaign.CampaignRepository {
	return &campaignRepository{db: db}
}

func (r *campaignRepository) Create(ctx context.Context, c *carriercampaign.Campaign) error {
	row, err := toCampaignRow(c)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	row.ID = newID(row.ID)
	row.CreatedAt, row.UpdatedAt = now, now
	if err := r.db.WithContext(ctx).Create(row).Error; err != nil {
		return err
	}
	c.ID, c.CreatedAt, c.UpdatedAt = row.ID, row.CreatedAt, row.UpdatedAt
	return nil
}

func (r *campaignRepository) Update(ctx context.Context, c *carriercampaign.Campaign, from carriercampaign.Status) error {
	if !c.Status.Editable() || !from.Editable() {
		return carriercampaign.ErrNotEditable
	}
	row, err := toCampaignRow(c)
	if err != nil {
		return err
	}
	row.UpdatedAt = time.Now().UTC()
	result := r.db.WithContext(ctx).Model(&schema.CarrierCampaign{}).
		Where("workspace_id = ? AND id = ? AND status = ?", c.WorkspaceID, c.ID, from).
		Updates(map[string]any{
			"name": row.Name, "sender_id": row.SenderID, "channel": row.Channel, "status": row.Status, "kind": row.Kind,
			"variants": row.Variants, "fallback": row.Fallback, "audience": row.Audience,
			"scheduled_at": row.ScheduledAt, "recurrence": row.Recurrence, "updated_at": row.UpdatedAt,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return r.refusal(ctx, c.WorkspaceID, c.ID)
	}
	c.UpdatedAt = row.UpdatedAt
	return nil
}

func (r *campaignRepository) refusal(ctx context.Context, workspaceID, id string) error {
	if _, err := r.FindByID(ctx, workspaceID, id); err != nil {
		return err
	}
	return carriercampaign.ErrNotEditable
}

func (r *campaignRepository) Delete(ctx context.Context, workspaceID, id string) error {
	if !wellFormedID(id) {
		return carriercampaign.ErrCampaignNotFound
	}
	result := r.db.WithContext(ctx).
		Where("workspace_id = ? AND id = ? AND status IN ?", workspaceID, id, carriercampaign.DeletableStatuses()).
		Delete(&schema.CarrierCampaign{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return r.refusal(ctx, workspaceID, id)
	}
	return nil
}

func (r *campaignRepository) take(query *gorm.DB) (*carriercampaign.Campaign, error) {
	var row schema.CarrierCampaign
	if err := query.Take(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, carriercampaign.ErrCampaignNotFound
		}
		return nil, err
	}
	return toCampaign(&row)
}

func (r *campaignRepository) FindByID(ctx context.Context, workspaceID, id string) (*carriercampaign.Campaign, error) {
	if !wellFormedID(id) {
		return nil, carriercampaign.ErrCampaignNotFound
	}
	return r.take(r.db.WithContext(ctx).Where("workspace_id = ? AND id = ?", workspaceID, id))
}

func (r *campaignRepository) FindByIDUnscoped(ctx context.Context, id string) (*carriercampaign.Campaign, error) {
	if !wellFormedID(id) {
		return nil, carriercampaign.ErrCampaignNotFound
	}
	return r.take(r.db.WithContext(ctx).Where("id = ?", id))
}

func (r *campaignRepository) List(ctx context.Context, in carriercampaign.ListCampaignsInput) (*shared.PaginatedResult[*carriercampaign.Campaign], error) {
	query := r.db.WithContext(ctx).Model(&schema.CarrierCampaign{}).Where("workspace_id = ?", in.WorkspaceID)
	if in.Channel != "" {
		query = query.Where("channel = ?", in.Channel)
	}
	if in.Status != "" {
		query = query.Where("status = ?", in.Status)
	}
	if in.Kind != "" {
		query = query.Where("kind = ?", in.Kind)
	}
	if in.SenderID != "" {
		query = query.Where("sender_id = ?", in.SenderID)
	}
	if search := strings.TrimSpace(in.Search); search != "" {
		query = query.Where(`name ILIKE ? ESCAPE '\'`, sqlexpr.LikeContains(search))
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, err
	}
	page := shared.NormalizePagination(in.Pagination)
	var rows []schema.CarrierCampaign
	if err := query.Order("created_at DESC, id DESC").Limit(page.PageSize).Offset(page.Offset()).Find(&rows).Error; err != nil {
		return nil, err
	}
	items, err := toCampaigns(rows)
	if err != nil {
		return nil, err
	}
	return shared.NewPaginatedResult(items, page, total), nil
}

func (r *campaignRepository) ListByStatus(ctx context.Context, statuses ...carriercampaign.Status) ([]*carriercampaign.Campaign, error) {
	var rows []schema.CarrierCampaign
	if err := r.db.WithContext(ctx).Where("status IN ?", statuses).Order("created_at ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	return toCampaigns(rows)
}

func (r *campaignRepository) ListScheduledDue(ctx context.Context, now time.Time, limit int) ([]*carriercampaign.Campaign, error) {
	var rows []schema.CarrierCampaign
	if err := r.db.WithContext(ctx).
		Where("status = ? AND scheduled_at <= ?", carriercampaign.StatusScheduled, now).
		Order("scheduled_at ASC").
		Limit(limit).
		Find(&rows).Error; err != nil {
		return nil, err
	}
	return toCampaigns(rows)
}

func (r *campaignRepository) SwapStatus(ctx context.Context, id string, from, to carriercampaign.Status) (bool, error) {
	if err := errors.Join(valid("campaign status", from), valid("campaign status", to)); err != nil {
		return false, err
	}
	result := r.db.WithContext(ctx).Model(&schema.CarrierCampaign{}).
		Where("id = ? AND status = ?", id, from).
		Update("status", to)
	return result.RowsAffected > 0, result.Error
}

func (r *campaignRepository) Reschedule(ctx context.Context, id string, from, to carriercampaign.Status, at *time.Time) (bool, error) {
	if err := errors.Join(valid("campaign status", from), valid("campaign status", to)); err != nil {
		return false, err
	}
	result := r.db.WithContext(ctx).Model(&schema.CarrierCampaign{}).
		Where("id = ? AND status = ?", id, from).
		Updates(map[string]any{"status": to, "scheduled_at": at})
	return result.RowsAffected > 0, result.Error
}

func (r *campaignRepository) ClaimScheduledStart(campaignID string, dueBy time.Time) (bool, error) {
	result := r.db.Model(&schema.CarrierCampaign{}).
		Where("id = ? AND status = ? AND scheduled_at IS NOT NULL AND scheduled_at <= ?", campaignID, carriercampaign.StatusScheduled, dueBy.UTC()).
		Update("scheduled_at", nil)
	return result.RowsAffected > 0, result.Error
}

func (r *campaignRepository) CountByStatus(ctx context.Context, workspaceID string) (map[carriercampaign.Status]int64, error) {
	var rows []struct {
		Status string
		Total  int64
	}
	if err := r.db.WithContext(ctx).Model(&schema.CarrierCampaign{}).
		Select("status, count(*) AS total").
		Where("workspace_id = ?", workspaceID).
		Group("status").
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[carriercampaign.Status]int64, len(rows))
	for _, row := range rows {
		status := carriercampaign.Status(row.Status)
		if err := valid("campaign status", status); err != nil {
			return nil, err
		}
		out[status] = row.Total
	}
	return out, nil
}
