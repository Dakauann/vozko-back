package carriermessaging_repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"vozko/domain/carriermessaging"
	"vozko/infra/database/schema"
)

type MessageRepository struct {
	db *gorm.DB
}

var _ carriermessaging.MessageRepository = (*MessageRepository)(nil)

func NewMessageRepository(db *gorm.DB) *MessageRepository {
	return &MessageRepository{db: db}
}

var (
	landedStatuses = filtered(carriermessaging.AllStatuses(), carriermessaging.Status.Landed)
	openStatuses   = filtered(carriermessaging.AllStatuses(), func(s carriermessaging.Status) bool { return !s.Landed() && !s.Terminal() })
)

func (r *MessageRepository) Create(ctx context.Context, m *carriermessaging.Message) error {
	record, err := toMessageSchema(m)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	record.ID = newID(record.ID)
	if record.CreatedAt.IsZero() {
		record.CreatedAt = now
	}
	record.UpdatedAt = now
	if err := r.db.WithContext(ctx).Create(record).Error; err != nil {
		return err
	}
	m.ID, m.CreatedAt = record.ID, record.CreatedAt
	return nil
}

func (r *MessageRepository) take(query *gorm.DB) (*carriermessaging.Message, error) {
	var record schema.CarrierMessage
	if err := query.Take(&record).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, carriermessaging.ErrMessageNotFound
		}
		return nil, err
	}
	return toMessage(&record)
}

func (r *MessageRepository) FindByID(ctx context.Context, id string) (*carriermessaging.Message, error) {
	return r.take(r.db.WithContext(ctx).Where("id = ?", id))
}

func (r *MessageRepository) FindByReference(ctx context.Context, reference string) (*carriermessaging.Message, error) {
	return r.take(r.db.WithContext(ctx).Where("reference = ?", reference))
}

func (r *MessageRepository) Advance(ctx context.Context, reference string, next carriermessaging.Status, errorCode string, at time.Time) (*carriermessaging.Message, bool, error) {
	var (
		out   *carriermessaging.Message
		moved bool
	)
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		current, err := r.take(tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("reference = ?", reference))
		if err != nil {
			return err
		}
		advanced, ok := current.Advance(next, errorCode, at)
		if !ok {
			out = current
			return nil
		}
		if err := tx.Model(&schema.CarrierMessage{}).Where("id = ?", current.ID).Updates(map[string]any{
			"status":       string(advanced.Status),
			"error_code":   advanced.ErrorCode,
			"accepted_at":  advanced.AcceptedAt,
			"delivered_at": advanced.DeliveredAt,
			"read_at":      advanced.ReadAt,
			"failed_at":    advanced.FailedAt,
		}).Error; err != nil {
			return err
		}
		out, moved = &advanced, true
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	return out, moved, nil
}

func (r *MessageRepository) LinkFallback(ctx context.Context, messageID, fallbackMessageID string) (bool, error) {
	result := r.db.WithContext(ctx).Model(&schema.CarrierMessage{}).
		Where("id = ? AND fallback_message_id IS NULL", messageID).
		Update("fallback_message_id", fallbackMessageID)
	return changedOnce(ctx, r.db, result, &schema.CarrierMessage{}, messageID, carriermessaging.ErrMessageNotFound)
}

func (r *MessageRepository) ListOverdue(ctx context.Context, now time.Time, limit int) ([]*carriermessaging.Message, error) {
	var records []schema.CarrierMessage
	if err := r.db.WithContext(ctx).
		Where("status IN ? AND expires_at <= ?", openStatuses, now).
		Order("expires_at ASC").
		Limit(limit).
		Find(&records).Error; err != nil {
		return nil, err
	}
	return toMessages(records)
}

func (r *MessageRepository) count(query *gorm.DB) (int, error) {
	var n int64
	if err := query.Model(&schema.CarrierMessage{}).Count(&n).Error; err != nil {
		return 0, err
	}
	return int(n), nil
}

func (r *MessageRepository) CountMarketingSince(ctx context.Context, workspaceID, phone string, since time.Time) (int, error) {
	return r.count(r.db.WithContext(ctx).Where(
		"workspace_id = ? AND phone = ? AND marketing = ? AND parent_message_id IS NULL AND status <> ? AND created_at >= ?",
		workspaceID, phone, true, carriermessaging.StatusRejected, since,
	))
}

func (r *MessageRepository) CountRunKindSince(ctx context.Context, workspaceID string, kind carriermessaging.RunKind, since time.Time) (int, error) {
	return r.count(r.db.WithContext(ctx).Where("workspace_id = ? AND run_kind = ? AND created_at >= ?", workspaceID, kind, since))
}

func (r *MessageRepository) newest(query *gorm.DB, order string) (*carriermessaging.Message, error) {
	var records []schema.CarrierMessage
	if err := query.Order(order).Limit(1).Find(&records).Error; err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return nil, nil
	}
	return toMessage(&records[0])
}

func (r *MessageRepository) LatestDeliveredTo(ctx context.Context, workspaceID, phone string, since time.Time) (*carriermessaging.Message, error) {
	return r.newest(r.db.WithContext(ctx).Where(
		"workspace_id = ? AND phone = ? AND parent_message_id IS NULL AND status IN ? AND delivered_at >= ?",
		workspaceID, phone, landedStatuses, since,
	), "delivered_at DESC")
}

func (r *MessageRepository) LatestSMSTo(ctx context.Context, phone string, since time.Time) (*carriermessaging.Message, error) {
	return r.newest(r.db.WithContext(ctx).Where(
		"phone = ? AND step = ? AND created_at >= ?", phone, carriermessaging.StepSMS, since,
	), "created_at DESC")
}

func (r *MessageRepository) RecordOperator(ctx context.Context, reference, operator string) error {
	result := r.db.WithContext(ctx).Model(&schema.CarrierMessage{}).
		Where("reference = ?", reference).
		Update("operator", optionalID(operator))
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return carriermessaging.ErrMessageNotFound
	}
	return nil
}
