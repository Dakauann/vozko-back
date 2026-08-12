package scheduled_message_repository

import (
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"

	sm "vozko/domain/scheduled_message"
	"vozko/infra/database/schema"
)

type repository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) sm.Repository {
	return &repository{db: db}
}

func (r *repository) Create(m *sm.ScheduledMessage) error {
	row := fromDomain(m)
	if err := r.db.Create(&row).Error; err != nil {
		return err
	}
	m.ID = row.ID
	m.CreatedAt = row.CreatedAt
	m.UpdatedAt = row.UpdatedAt
	return nil
}

func (r *repository) FindByID(id string) (*sm.ScheduledMessage, error) {
	var row schema.ScheduledMessage
	if err := r.db.Where("id = ?", id).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, sm.ErrNotFound
		}
		return nil, err
	}
	return toDomain(&row), nil
}

func (r *repository) FindByIdempotencyKey(workspaceID, key string) (*sm.ScheduledMessage, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, sm.ErrNotFound
	}

	var row schema.ScheduledMessage
	err := r.db.Where("workspace_id = ? AND idempotency_key = ?", workspaceID, key).First(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, sm.ErrNotFound
		}
		return nil, err
	}
	return toDomain(&row), nil
}

func (r *repository) ListByEntry(entryID, entryType string, statuses []sm.Status) ([]*sm.ScheduledMessage, error) {
	query := r.db.Where("entry_id = ? AND entry_type = ?", entryID, entryType)
	if len(statuses) > 0 {
		query = query.Where("status IN ?", statusStrings(statuses))
	}

	var rows []schema.ScheduledMessage
	// Ascending: the composer's panel reads as a queue, and the next message to
	// go out is the one the operator cares about.
	if err := query.Order("scheduled_at ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	return toDomainSlice(rows), nil
}

func (r *repository) ListByWorkspace(workspaceID string, q sm.ListQuery) ([]*sm.ScheduledMessage, int64, error) {
	base := r.db.Model(&schema.ScheduledMessage{}).Where("workspace_id = ?", workspaceID)
	if len(q.Statuses) > 0 {
		base = base.Where("status IN ?", statusStrings(q.Statuses))
	}

	var total int64
	if err := base.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	page, pageSize := q.Page, q.PageSize
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 200 {
		pageSize = 50
	}

	var rows []schema.ScheduledMessage
	err := base.Order("scheduled_at ASC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&rows).Error
	if err != nil {
		return nil, 0, err
	}
	return toDomainSlice(rows), total, nil
}

// ClaimForDispatch is the whole at-most-once guarantee.
//
// One conditional UPDATE, guarded on the row still being pending, whose
// affected-row count decides the winner. Two dispatchers racing on the same id
// cannot both see RowsAffected == 1, so they cannot both send — and the same
// guard is what makes a cancel racing a dispatch resolve cleanly instead of
// producing both a cancellation and a delivery.
//
// Read-then-write would NOT do this: both callers would observe `pending` and
// both would proceed. RETURNING is used so the winner gets the row without a
// second query that could observe a later state.
func (r *repository) ClaimForDispatch(id string, now time.Time) (*sm.ScheduledMessage, error) {
	var rows []schema.ScheduledMessage

	// RETURNING rather than update-then-read: a second SELECT could observe a
	// state written after the claim, and the winner must see exactly the row it
	// won.
	err := r.db.Raw(`
		UPDATE scheduled_messages
		   SET status = ?, claimed_at = ?, updated_at = ?
		 WHERE id = ? AND status = ? AND deleted_at IS NULL
		RETURNING *`,
		string(sm.StatusSending), now, now, id, string(sm.StatusPending),
	).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		// Gone, already claimed, cancelled, or already sent. Every one of those
		// means "not ours", and none of them is an error.
		return nil, nil
	}
	return toDomain(&rows[0]), nil
}

// ClaimDueBatch claims every message past due, bounded.
//
// The subquery uses FOR UPDATE SKIP LOCKED so two replicas sweeping at the same
// instant take DISJOINT batches instead of one blocking on the other and then
// finding every row already claimed. The outer conditional UPDATE still carries
// the status guard, so correctness never depends on the locking hint.
func (r *repository) ClaimDueBatch(now time.Time, limit int) ([]*sm.ScheduledMessage, error) {
	if limit < 1 {
		limit = 1
	}

	var rows []schema.ScheduledMessage
	err := r.db.Raw(`
		UPDATE scheduled_messages
		   SET status = ?, claimed_at = ?, updated_at = ?
		 WHERE id IN (
		       SELECT id FROM scheduled_messages
		        WHERE status = ? AND scheduled_at <= ? AND deleted_at IS NULL
		        ORDER BY scheduled_at ASC
		        LIMIT ?
		        FOR UPDATE SKIP LOCKED
		 )
		RETURNING *`,
		string(sm.StatusSending), now, now,
		string(sm.StatusPending), now, limit,
	).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	return toDomainSlice(rows), nil
}

func (r *repository) MarkSent(id, messageID string, sentAt time.Time) error {
	return r.transitionFrom(id, sm.StatusSending, map[string]interface{}{
		"status":          string(sm.StatusSent),
		"sent_message_id": messageID,
		"sent_at":         sentAt,
		"updated_at":      sentAt,
	})
}

// MarkFailed accepts a row in either pre-dispatch state.
//
// pending is legal here because the sweep retires long-overdue messages that
// were never claimed; sending is the ordinary path from a failed dispatch.
func (r *repository) MarkFailed(id string, reason sm.FailureReason, detail string) error {
	now := time.Now().UTC()
	result := r.db.Model(&schema.ScheduledMessage{}).
		Where("id = ? AND status IN ? AND deleted_at IS NULL",
			id, []string{string(sm.StatusSending), string(sm.StatusPending)}).
		Updates(map[string]interface{}{
			"status":         string(sm.StatusFailed),
			"failure_reason": string(reason),
			"failure_detail": detail,
			"updated_at":     now,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return sm.ErrNotPending
	}
	return nil
}

func (r *repository) Cancel(id string) error {
	return r.transitionFrom(id, sm.StatusPending, map[string]interface{}{
		"status":     string(sm.StatusCanceled),
		"updated_at": time.Now().UTC(),
	})
}

func (r *repository) Reschedule(id string, at time.Time, windowExpiresAt *time.Time) error {
	return r.transitionFrom(id, sm.StatusPending, map[string]interface{}{
		"scheduled_at":                  at,
		"window_expires_at_at_creation": windowExpiresAt,
		"updated_at":                    time.Now().UTC(),
	})
}

// transitionFrom is the shared conditional write. Every state change in this
// repository goes through it, so none of them can accidentally be written as an
// unguarded update.
func (r *repository) transitionFrom(id string, from sm.Status, updates map[string]interface{}) error {
	result := r.db.Model(&schema.ScheduledMessage{}).
		Where("id = ? AND status = ? AND deleted_at IS NULL", id, string(from)).
		Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return sm.ErrNotPending
	}
	return nil
}

func (r *repository) ListStuckClaims(claimedBefore time.Time, limit int) ([]*sm.ScheduledMessage, error) {
	var rows []schema.ScheduledMessage
	err := r.db.Where("status = ? AND claimed_at IS NOT NULL AND claimed_at < ? AND deleted_at IS NULL",
		string(sm.StatusSending), claimedBefore).
		Order("claimed_at ASC").
		Limit(limit).
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	return toDomainSlice(rows), nil
}

// PurgeTerminalBefore deletes finished rows. Pending and in-flight rows are
// never touched, however old: an undelivered message is not litter.
func (r *repository) PurgeTerminalBefore(cutoff time.Time) (int64, error) {
	result := r.db.Unscoped().
		Where("status IN ? AND updated_at < ?",
			[]string{string(sm.StatusSent), string(sm.StatusFailed), string(sm.StatusCanceled)}, cutoff).
		Delete(&schema.ScheduledMessage{})
	return result.RowsAffected, result.Error
}

var _ sm.Repository = (*repository)(nil)
