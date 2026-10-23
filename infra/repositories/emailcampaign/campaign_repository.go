package emailcampaign

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"vozko/domain/campaign"
	domain "vozko/domain/emailcampaign"
	"vozko/infra/database"
	"vozko/infra/database/schema"
)

const (
	defaultListLimit = 50
	maxListLimit     = 200
)

func statusNames(statuses []domain.Status) []string {
	out := make([]string, 0, len(statuses))
	for _, s := range statuses {
		out = append(out, string(s))
	}
	return out
}

type CampaignRepository struct {
	db  *gorm.DB
	now func() time.Time
}

func NewCampaignRepository(db *gorm.DB) *CampaignRepository {
	return &CampaignRepository{db: db, now: func() time.Time { return time.Now().UTC() }}
}

func (r *CampaignRepository) Create(ctx context.Context, c *domain.Campaign) error {
	if c.ID == "" {
		c.ID = uuid.NewString()
	}
	now := r.now()
	c.CreatedAt, c.UpdatedAt = now, now
	row, err := toRow(c)
	if err != nil {
		return err
	}
	return r.db.WithContext(ctx).Create(&row).Error
}

func (r *CampaignRepository) Get(ctx context.Context, workspaceID, id string) (*domain.Campaign, error) {
	if len(database.UUIDArray([]string{id})) == 0 {
		return nil, domain.ErrCampaignNotFound
	}
	return r.first(r.db.WithContext(ctx).Where("workspace_id = ? AND id = ?", workspaceID, id))
}

func (r *CampaignRepository) GetByID(ctx context.Context, id string) (*domain.Campaign, error) {
	if len(database.UUIDArray([]string{id})) == 0 {
		return nil, domain.ErrCampaignNotFound
	}
	return r.first(r.db.WithContext(ctx).Where("id = ?", id))
}

func (r *CampaignRepository) first(q *gorm.DB) (*domain.Campaign, error) {
	var row schema.EmailCampaign
	if err := q.First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrCampaignNotFound
		}
		return nil, err
	}
	c, err := toDomain(row)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *CampaignRepository) Update(ctx context.Context, c *domain.Campaign, from domain.Status) error {
	row, err := toRow(c)
	if err != nil {
		return err
	}
	c.UpdatedAt = r.now()
	result := r.db.WithContext(ctx).Model(&schema.EmailCampaign{}).
		Where("workspace_id = ? AND id = ? AND status = ? AND status IN ?", c.WorkspaceID, c.ID, string(from), statusNames(domain.EditableStatuses())).
		Updates(map[string]any{
			"status": row.Status,
			"name":   row.Name, "sender_id": row.SenderID, "subject": row.Subject, "preheader": row.Preheader,
			"document": row.Document, "selection": row.Selection, "list_id": row.ListID, "merge_policies": row.MergePolicies, "ab_test": row.ABTest, "utm": row.UTM,
			"updated_at": c.UpdatedAt,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return r.refusal(ctx, c.WorkspaceID, c.ID)
	}
	return nil
}

func (r *CampaignRepository) refusal(ctx context.Context, workspaceID, id string) error {
	if _, err := r.Get(ctx, workspaceID, id); err != nil {
		return err
	}
	return domain.ErrNotEditable
}

func (r *CampaignRepository) Delete(ctx context.Context, workspaceID, id string) error {
	if len(database.UUIDArray([]string{id})) == 0 {
		return domain.ErrCampaignNotFound
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Where("workspace_id = ? AND id = ? AND status IN ?", workspaceID, id, statusNames(domain.DeletableStatuses())).Delete(&schema.EmailCampaign{})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return r.refusal(ctx, workspaceID, id)
		}
		return tx.Where("campaign_id = ?", id).Delete(&schema.EmailCampaignRecipient{}).Error
	})
}

func (r *CampaignRepository) List(ctx context.Context, workspaceID string, q domain.ListQuery) ([]domain.Campaign, int64, error) {
	base := r.db.WithContext(ctx).Model(&schema.EmailCampaign{}).Where("workspace_id = ?", workspaceID)
	if q.Status != "" {
		base = base.Where("status = ?", string(q.Status))
	}
	if search := strings.TrimSpace(q.Search); search != "" {
		base = base.Where("name ILIKE ?", "%"+escapeLike(search)+"%")
	}
	var total int64
	if err := base.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	limit := q.Limit
	if limit <= 0 {
		limit = defaultListLimit
	}
	limit = min(limit, maxListLimit)
	var rows []schema.EmailCampaign
	if err := base.Order("created_at DESC, id DESC").Offset(max(q.Offset, 0)).Limit(limit).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	out, err := toDomainAll(rows)
	return out, total, err
}

func toDomainAll(rows []schema.EmailCampaign) ([]domain.Campaign, error) {
	out := make([]domain.Campaign, 0, len(rows))
	for _, row := range rows {
		c, err := toDomain(row)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

func (r *CampaignRepository) swap(ctx context.Context, id string, from domain.Status, values map[string]any) (bool, error) {
	values["updated_at"] = r.now()
	result := r.db.WithContext(ctx).Model(&schema.EmailCampaign{}).Where("id = ? AND status = ?", id, string(from)).Updates(values)
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

func (r *CampaignRepository) SwapStatus(ctx context.Context, id string, from, to domain.Status, reason domain.HoldReason) (bool, error) {
	return r.swap(ctx, id, from, map[string]any{"status": string(to), "hold_reason": string(reason)})
}

func (r *CampaignRepository) Schedule(ctx context.Context, id string, from domain.Status, at time.Time, launchedBy string) (bool, error) {
	return r.swap(ctx, id, from, map[string]any{
		"status": string(domain.StatusScheduled), "scheduled_at": at.UTC(), "launched_by": schema.OptionalText(launchedBy), "hold_reason": "",
	})
}

func (r *CampaignRepository) MarkLaunched(ctx context.Context, id string, from domain.Status, l domain.Launch) (bool, error) {
	return r.swap(ctx, id, from, map[string]any{
		"status": string(domain.StatusRunning), "started_at": l.StartedAt.UTC(),
		"launched_by": schema.OptionalText(l.LaunchedBy), "hold_reason": "", "scheduled_at": nil,
	})
}

func (r *CampaignRepository) SetWinner(ctx context.Context, id string, winner domain.Variant) (bool, error) {
	result := r.db.WithContext(ctx).Exec(
		"UPDATE email_campaigns SET ab_test = jsonb_set(ab_test, '{winner}', to_jsonb(?::text)), updated_at = ?"+
			" WHERE id = ? AND ab_test IS NOT NULL AND COALESCE(ab_test->>'winner', '') = ''",
		string(winner), r.now(), id)
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

func (r *CampaignRepository) Complete(ctx context.Context, id string, at time.Time) (bool, error) {
	return r.swap(ctx, id, domain.StatusRunning, map[string]any{"status": string(domain.StatusCompleted), "completed_at": at.UTC(), "hold_reason": ""})
}

func (r *CampaignRepository) ListByStatus(ctx context.Context, statuses ...domain.Status) ([]domain.Campaign, error) {
	if len(statuses) == 0 {
		return []domain.Campaign{}, nil
	}
	values := make([]string, len(statuses))
	for i, s := range statuses {
		values[i] = string(s)
	}
	var rows []schema.EmailCampaign
	if err := r.db.WithContext(ctx).Where("status IN ?", values).Order("created_at").Find(&rows).Error; err != nil {
		return nil, err
	}
	return toDomainAll(rows)
}

func (r *CampaignRepository) ListScheduledDue(ctx context.Context, now time.Time, limit int) ([]domain.Campaign, error) {
	var rows []schema.EmailCampaign
	err := r.db.WithContext(ctx).
		Where("status = ? AND scheduled_at IS NOT NULL AND scheduled_at <= ?", string(domain.StatusScheduled), now.UTC()).
		Order("scheduled_at").Limit(limit).Find(&rows).Error
	if err != nil {
		return nil, err
	}
	return toDomainAll(rows)
}

func (r *CampaignRepository) ListAwaitingWinner(ctx context.Context, now time.Time, limit int) ([]domain.Campaign, error) {
	var rows []schema.EmailCampaign
	err := r.db.WithContext(ctx).
		Where("status IN ? AND ab_test IS NOT NULL AND jsonb_typeof(ab_test) = 'object' AND COALESCE(ab_test->>'winner', '') = ''",
			[]string{string(domain.StatusRunning), string(domain.StatusHeld)}).
		Where("started_at IS NOT NULL AND started_at + make_interval(mins => (ab_test->>'decide_after_minutes')::int) <= ?", now.UTC()).
		Order("started_at").Limit(limit).Find(&rows).Error
	if err != nil {
		return nil, err
	}
	return toDomainAll(rows)
}

func (r *CampaignRepository) ClaimScheduledStart(campaignID string, dueBy time.Time) (bool, error) {
	result := r.db.Model(&schema.EmailCampaign{}).
		Where("id = ? AND status = ? AND scheduled_at IS NOT NULL AND scheduled_at <= ?", campaignID, string(domain.StatusScheduled), dueBy.UTC()).
		Update("scheduled_at", nil)
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

func (r *CampaignRepository) Names(ctx context.Context, workspaceID string, ids []string) (map[string]string, error) {
	valid := database.UUIDArray(ids)
	out := make(map[string]string, len(valid))
	if len(valid) == 0 {
		return out, nil
	}
	var rows []struct {
		ID   string
		Name string
	}
	if err := r.db.WithContext(ctx).Model(&schema.EmailCampaign{}).Select("id, name").
		Where("workspace_id = ? AND id IN ?", workspaceID, []string(valid)).Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		out[row.ID] = row.Name
	}
	return out, nil
}

var (
	_ domain.Repository        = (*CampaignRepository)(nil)
	_ campaign.ScheduleClaimer = (*CampaignRepository)(nil)
)

func (r *CampaignRepository) SetSettlement(ctx context.Context, id string, s domain.RunSettlement) (bool, error) {
	result := r.db.WithContext(ctx).Model(&schema.EmailCampaign{}).Where("id = ? AND settled_at IS NULL", id).Updates(map[string]any{
		"refunded_units": s.RefundedUnits, "refunded_micros": s.RefundedMicros, "settled_at": s.SettledAt.UTC(), "updated_at": r.now(),
	})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

func (r *CampaignRepository) SetTestBatch(ctx context.Context, id string, from domain.TestBatchState, tb domain.TestBatch) (bool, error) {
	result := r.db.WithContext(ctx).Model(&schema.EmailCampaign{}).
		Where("id = ? AND test_batch_state = ?", id, string(from)).
		Updates(map[string]any{
			"test_batch_size": tb.Size, "test_batch_state": string(tb.State), "test_batch_last_id": tb.LastID,
			"test_batch_drained_at": utc(tb.DrainedAt), "test_batch_decided_at": utc(tb.DecidedAt), "updated_at": r.now(),
		})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

func (r *CampaignRepository) ListTesting(ctx context.Context, limit int) ([]domain.Campaign, error) {
	var rows []schema.EmailCampaign
	err := r.db.WithContext(ctx).
		Where("test_batch_state = ? AND status IN ?", string(domain.TestBatchTesting), []string{string(domain.StatusRunning), string(domain.StatusHeld)}).
		Order("started_at").Limit(limit).Find(&rows).Error
	if err != nil {
		return nil, err
	}
	return toDomainAll(rows)
}

func (r *CampaignRepository) FailedTestBatches(ctx context.Context, workspaceID string, since time.Time) (int, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&schema.EmailCampaign{}).
		Where("workspace_id = ? AND test_batch_state = ? AND test_batch_decided_at >= ?", workspaceID, string(domain.TestBatchFailed), since.UTC()).
		Count(&n).Error
	return int(n), err
}
