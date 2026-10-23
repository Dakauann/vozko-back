package carriercampaign_repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"vozko/domain/campaign"
	"vozko/domain/carriercampaign"
	"vozko/domain/shared"
	"vozko/infra/database/schema"
)

var activeRunStatuses = []campaign.Status{campaign.StatusRunning, campaign.StatusPaused}

type runRepository struct {
	db *gorm.DB
}

func NewRunRepository(db *gorm.DB) carriercampaign.RunRepository {
	return &runRepository{db: db}
}

func validRunStatus(s campaign.Status) error {
	if s.IsValid() {
		return nil
	}
	return fmt.Errorf("%w: run status %q", errUnknownStoredValue, s)
}

func toRun(row *schema.CarrierCampaignRun) (*carriercampaign.Run, error) {
	r := &carriercampaign.Run{
		ID:          row.ID,
		WorkspaceID: row.WorkspaceID,
		CampaignID:  row.CampaignID,
		Occurrence:  row.Occurrence,
		Status:      campaign.Status(row.Status),
		StartedAt:   row.StartedAt,
		CompletedAt: row.CompletedAt,
		CreatedAt:   row.CreatedAt,
		UpdatedAt:   row.UpdatedAt,
	}
	if err := validRunStatus(r.Status); err != nil {
		return nil, fmt.Errorf("carrier campaign run %s: %w", row.ID, err)
	}
	return r, nil
}

func toRuns(rows []schema.CarrierCampaignRun) ([]*carriercampaign.Run, error) {
	out := make([]*carriercampaign.Run, 0, len(rows))
	for i := range rows {
		r, err := toRun(&rows[i])
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}

func (r *runRepository) Create(ctx context.Context, run *carriercampaign.Run) (bool, error) {
	if err := validRunStatus(run.Status); err != nil {
		return false, err
	}
	now := time.Now().UTC()
	row := &schema.CarrierCampaignRun{
		ID:          newID(run.ID),
		WorkspaceID: run.WorkspaceID,
		CampaignID:  run.CampaignID,
		Occurrence:  run.Occurrence,
		Status:      string(run.Status),
		StartedAt:   run.StartedAt,
		CompletedAt: run.CompletedAt,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	result := r.db.WithContext(ctx).
		Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "campaign_id"}, {Name: "occurrence"}}, DoNothing: true}).
		Create(row)
	if result.Error != nil {
		return false, result.Error
	}
	if result.RowsAffected == 0 {
		return false, nil
	}
	run.ID, run.CreatedAt, run.UpdatedAt = row.ID, row.CreatedAt, row.UpdatedAt
	return true, nil
}

func (r *runRepository) take(query *gorm.DB) (*carriercampaign.Run, error) {
	var row schema.CarrierCampaignRun
	if err := query.Take(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, carriercampaign.ErrRunNotFound
		}
		return nil, err
	}
	return toRun(&row)
}

func (r *runRepository) FindByID(ctx context.Context, id string) (*carriercampaign.Run, error) {
	if !wellFormedID(id) {
		return nil, carriercampaign.ErrRunNotFound
	}
	return r.take(r.db.WithContext(ctx).Where("id = ?", id))
}

func (r *runRepository) FindByOccurrence(ctx context.Context, campaignID string, occurrence time.Time) (*carriercampaign.Run, error) {
	return r.take(r.db.WithContext(ctx).Where("campaign_id = ? AND occurrence = ?", campaignID, occurrence))
}

func (r *runRepository) FindActive(ctx context.Context, campaignID string) (*carriercampaign.Run, error) {
	return r.take(r.db.WithContext(ctx).Where("campaign_id = ? AND status IN ?", campaignID, activeRunStatuses).Order("occurrence DESC"))
}

func (r *runRepository) ListByCampaign(ctx context.Context, campaignID string, page shared.Pagination) (*shared.PaginatedResult[*carriercampaign.Run], error) {
	query := r.db.WithContext(ctx).Model(&schema.CarrierCampaignRun{}).Where("campaign_id = ?", campaignID)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, err
	}
	page = shared.NormalizePagination(page)
	var rows []schema.CarrierCampaignRun
	if err := query.Order("occurrence DESC, id DESC").Limit(page.PageSize).Offset(page.Offset()).Find(&rows).Error; err != nil {
		return nil, err
	}
	items, err := toRuns(rows)
	if err != nil {
		return nil, err
	}
	return shared.NewPaginatedResult(items, page, total), nil
}

func (r *runRepository) ListByStatus(ctx context.Context, statuses ...campaign.Status) ([]*carriercampaign.Run, error) {
	var rows []schema.CarrierCampaignRun
	if err := r.db.WithContext(ctx).Where("status IN ?", statuses).Order("created_at ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	return toRuns(rows)
}

func (r *runRepository) SwapStatus(ctx context.Context, id string, from, to campaign.Status) (bool, error) {
	if err := errors.Join(validRunStatus(from), validRunStatus(to)); err != nil {
		return false, err
	}
	result := r.db.WithContext(ctx).Model(&schema.CarrierCampaignRun{}).
		Where("id = ? AND status = ?", id, from).
		Update("status", to)
	return result.RowsAffected > 0, result.Error
}

func (r *runRepository) Complete(ctx context.Context, id string, at time.Time) (bool, error) {
	result := r.db.WithContext(ctx).Model(&schema.CarrierCampaignRun{}).
		Where("id = ? AND status = ?", id, campaign.StatusRunning).
		Updates(map[string]any{"status": campaign.StatusCompleted, "completed_at": at})
	return result.RowsAffected > 0, result.Error
}

func (r *runRepository) CountByStatus(ctx context.Context, campaignID string) (map[campaign.Status]int64, error) {
	var rows []struct {
		Status string
		Total  int64
	}
	if err := r.db.WithContext(ctx).Model(&schema.CarrierCampaignRun{}).
		Select("status, count(*) AS total").
		Where("campaign_id = ?", campaignID).
		Group("status").
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[campaign.Status]int64, len(rows))
	for _, row := range rows {
		status := campaign.Status(row.Status)
		if err := validRunStatus(status); err != nil {
			return nil, err
		}
		out[status] = row.Total
	}
	return out, nil
}
