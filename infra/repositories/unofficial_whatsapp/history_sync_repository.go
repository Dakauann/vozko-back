package unofficial_whatsapp_repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	uw "vozko/domain/unofficial_whatsapp"
	"vozko/infra/database"
	"vozko/infra/database/schema"
)

type historySyncRepository struct {
	db *gorm.DB
}

func NewHistorySyncRepository(db *gorm.DB) uw.HistorySyncRepository {
	return &historySyncRepository{db: db}
}

func (r *historySyncRepository) Create(ctx context.Context, s *uw.HistorySync) error {
	record := toHistorySyncSchema(s)
	if err := r.db.WithContext(ctx).Create(record).Error; err != nil {
		if database.IsUniqueViolation(err) {
			return uw.ErrHistorySyncActive
		}
		return err
	}
	s.ID = record.ID
	s.CreatedAt = record.CreatedAt
	s.UpdatedAt = record.UpdatedAt
	return nil
}

func (r *historySyncRepository) FindActive(ctx context.Context, instanceID string) (*uw.HistorySync, error) {
	return r.findOne(ctx, r.db.WithContext(ctx).
		Where("instance_id = ? AND status IN ?", instanceID, activeHistoryStatuses()))
}

func (r *historySyncRepository) FindLatest(ctx context.Context, instanceID string) (*uw.HistorySync, error) {
	return r.findOne(ctx, r.db.WithContext(ctx).Where("instance_id = ?", instanceID))
}

func (r *historySyncRepository) findOne(_ context.Context, q *gorm.DB) (*uw.HistorySync, error) {
	var record schema.UnofficialWhatsAppHistorySync
	err := q.Order("created_at DESC").Limit(1).Take(&record).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, uw.ErrHistorySyncNotFound
	}
	if err != nil {
		return nil, err
	}
	return toHistorySyncDomain(&record), nil
}

func (r *historySyncRepository) Resume(ctx context.Context, id string, now, pollUntil time.Time) error {
	result := r.db.WithContext(ctx).Model(&schema.UnofficialWhatsAppHistorySync{}).
		Where("id = ? AND status IN ?", id, activeHistoryStatuses()).
		Updates(map[string]any{
			"next_poll_at": gorm.Expr("LEAST(next_poll_at, ?)", now),
			"poll_until":   gorm.Expr("GREATEST(poll_until, ?)", pollUntil),
			"paused_since": nil,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return uw.ErrHistorySyncNotFound
	}
	return nil
}

func (r *historySyncRepository) ClaimDue(
	ctx context.Context,
	now time.Time,
	owner string,
	leaseUntil time.Time,
	limit int,
) ([]*uw.HistorySync, error) {
	if limit < 1 {
		limit = 1
	}
	var rows []schema.UnofficialWhatsAppHistorySync
	err := r.db.WithContext(ctx).Raw(`
		UPDATE unofficial_whatsapp_history_syncs
		   SET status = ?, lease_owner = ?, lease_until = ?,
		       started_at = COALESCE(started_at, ?), updated_at = ?
		 WHERE id IN (
		       SELECT id FROM unofficial_whatsapp_history_syncs
		        WHERE status IN ? AND next_poll_at <= ?
		          AND (lease_until IS NULL OR lease_until < ?)
		        ORDER BY next_poll_at ASC
		        LIMIT ?
		        FOR UPDATE SKIP LOCKED
		 )
		RETURNING *`,
		string(uw.HistorySyncRunning), owner, leaseUntil, now, now,
		activeHistoryStatuses(), now, now, limit,
	).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]*uw.HistorySync, 0, len(rows))
	for i := range rows {
		out = append(out, toHistorySyncDomain(&rows[i]))
	}
	return out, nil
}

func (r *historySyncRepository) Save(ctx context.Context, s *uw.HistorySync) error {
	if s.LeaseOwner == "" {
		return uw.ErrHistorySyncLeaseLost
	}
	result := r.db.WithContext(ctx).Model(&schema.UnofficialWhatsAppHistorySync{}).
		Where("id = ? AND lease_owner = ?", s.ID, s.LeaseOwner).
		Updates(map[string]any{
			"status":             string(s.Status),
			"oldest_message_at":  s.OldestMessageAt,
			"newest_message_at":  s.NewestMessageAt,
			"passes":             s.Passes,
			"quiet_passes":       s.QuietPasses,
			"messages_seen":      s.MessagesSeen,
			"messages_imported":  s.MessagesImported,
			"messages_duplicate": s.MessagesDuplicate,
			"messages_skipped":   s.MessagesSkipped,
			"messages_failed":    s.MessagesFailed,
			"attempts":           s.Attempts,
			"next_poll_at":       s.NextPollAt,
			"poll_until":         gorm.Expr("GREATEST(poll_until, ?)", s.PollUntil),
			"paused_since":       s.PausedSince,
			"reason":             truncate(s.Reason, 255),
			"last_error":         truncate(s.LastError, 500),
			"finished_at":        s.FinishedAt,
			"lease_owner":        "",
			"lease_until":        nil,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return uw.ErrHistorySyncLeaseLost
	}
	s.LeaseOwner = ""
	s.LeaseUntil = nil
	return nil
}

func activeHistoryStatuses() []string {
	statuses := uw.ActiveHistorySyncStatuses()
	out := make([]string, 0, len(statuses))
	for _, s := range statuses {
		out = append(out, string(s))
	}
	return out
}

func toHistorySyncSchema(s *uw.HistorySync) *schema.UnofficialWhatsAppHistorySync {
	return &schema.UnofficialWhatsAppHistorySync{
		ID:                s.ID,
		WorkspaceID:       s.WorkspaceID,
		InstanceID:        s.InstanceID,
		Status:            string(s.Status),
		Trigger:           string(s.Trigger),
		WindowFrom:        s.WindowFrom,
		OldestMessageAt:   s.OldestMessageAt,
		NewestMessageAt:   s.NewestMessageAt,
		Passes:            s.Passes,
		QuietPasses:       s.QuietPasses,
		MessagesSeen:      s.MessagesSeen,
		MessagesImported:  s.MessagesImported,
		MessagesDuplicate: s.MessagesDuplicate,
		MessagesSkipped:   s.MessagesSkipped,
		MessagesFailed:    s.MessagesFailed,
		Attempts:          s.Attempts,
		NextPollAt:        s.NextPollAt,
		PollUntil:         s.PollUntil,
		PausedSince:       s.PausedSince,
		LeaseOwner:        s.LeaseOwner,
		LeaseUntil:        s.LeaseUntil,
		Reason:            truncate(s.Reason, 255),
		LastError:         truncate(s.LastError, 500),
		StartedAt:         s.StartedAt,
		FinishedAt:        s.FinishedAt,
	}
}

func toHistorySyncDomain(r *schema.UnofficialWhatsAppHistorySync) *uw.HistorySync {
	return &uw.HistorySync{
		ID:                r.ID,
		WorkspaceID:       r.WorkspaceID,
		InstanceID:        r.InstanceID,
		Status:            uw.HistorySyncStatus(r.Status),
		Trigger:           uw.HistorySyncTrigger(r.Trigger),
		WindowFrom:        r.WindowFrom.UTC(),
		OldestMessageAt:   r.OldestMessageAt,
		NewestMessageAt:   r.NewestMessageAt,
		Passes:            r.Passes,
		QuietPasses:       r.QuietPasses,
		MessagesSeen:      r.MessagesSeen,
		MessagesImported:  r.MessagesImported,
		MessagesDuplicate: r.MessagesDuplicate,
		MessagesSkipped:   r.MessagesSkipped,
		MessagesFailed:    r.MessagesFailed,
		Attempts:          r.Attempts,
		NextPollAt:        r.NextPollAt.UTC(),
		PollUntil:         r.PollUntil.UTC(),
		PausedSince:       r.PausedSince,
		LeaseOwner:        r.LeaseOwner,
		LeaseUntil:        r.LeaseUntil,
		Reason:            r.Reason,
		LastError:         r.LastError,
		StartedAt:         r.StartedAt,
		FinishedAt:        r.FinishedAt,
		CreatedAt:         r.CreatedAt,
		UpdatedAt:         r.UpdatedAt,
	}
}
