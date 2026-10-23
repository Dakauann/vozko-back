package carriermessaging_repository

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"vozko/domain/carriermessaging"
	"vozko/infra/database/schema"
)

var errEventDedupKeyMissing = errors.New("rcs: an event needs a dedup key")

type eventRepository struct {
	db *gorm.DB
}

func NewEventRepository(db *gorm.DB) carriermessaging.EventRepository {
	return &eventRepository{db: db}
}

func (r *eventRepository) Record(ctx context.Context, e *carriermessaging.MessageEvent) (bool, error) {
	if err := valid("event kind", e.Kind); err != nil {
		return false, err
	}
	if strings.TrimSpace(e.DedupKey) == "" {
		return false, errEventDedupKeyMissing
	}
	now := time.Now().UTC()
	record := &schema.CarrierMessageEvent{
		ID:          newID(e.ID),
		WorkspaceID: e.WorkspaceID,
		MessageID:   optionalID(e.MessageID),
		SenderID:    optionalID(e.SenderID),
		Phone:       e.Phone,
		Kind:        string(e.Kind),
		Label:       e.Label,
		DedupKey:    e.DedupKey,
		OccurredAt:  e.OccurredAt,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if e.Button != nil {
		record.ButtonCard, record.ButtonIndex = &e.Button.Card, &e.Button.Index
	}
	result := r.db.WithContext(ctx).
		Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "dedup_key"}}, DoNothing: true}).
		Create(record)
	if result.Error != nil {
		return false, result.Error
	}
	if result.RowsAffected == 0 {
		return false, nil
	}
	e.ID = record.ID
	return true, nil
}

func (r *eventRepository) CountSince(ctx context.Context, workspaceID, phone string, kind carriermessaging.EventKind, since time.Time) (int, error) {
	var n int64
	if err := r.db.WithContext(ctx).Model(&schema.CarrierMessageEvent{}).
		Where("workspace_id = ? AND phone = ? AND kind = ? AND occurred_at >= ?", workspaceID, phone, kind, since).
		Count(&n).Error; err != nil {
		return 0, err
	}
	return int(n), nil
}
