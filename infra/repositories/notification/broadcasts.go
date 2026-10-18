package notification_repository

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"gorm.io/gorm"

	"vozko/domain/notification"
	"vozko/domain/shared"
	"vozko/infra/database/schema"
)

type BroadcastStore struct {
	db *gorm.DB
}

var _ notification.BroadcastStore = (*BroadcastStore)(nil)

func NewBroadcastStore(db *gorm.DB) *BroadcastStore {
	return &BroadcastStore{db: db}
}

func (s *BroadcastStore) CreateBroadcast(ctx context.Context, b notification.Broadcast) (notification.Broadcast, error) {
	row, err := broadcastRow(b)
	if err != nil {
		return notification.Broadcast{}, err
	}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		return notification.Broadcast{}, err
	}
	return broadcastOf(row)
}

func (s *BroadcastStore) Broadcast(ctx context.Context, id string) (notification.Broadcast, error) {
	if !validUUID(id) {
		return notification.Broadcast{}, notification.ErrNotFound
	}
	var row schema.NotificationBroadcast
	err := s.db.WithContext(ctx).Where("id = ?", id).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return notification.Broadcast{}, notification.ErrNotFound
	}
	if err != nil {
		return notification.Broadcast{}, err
	}
	return broadcastOf(row)
}

func (s *BroadcastStore) CompleteBroadcast(ctx context.Context, id string, workspaces int, at time.Time) error {
	return s.db.WithContext(ctx).Model(&schema.NotificationBroadcast{}).
		Where("id = ? AND status = ?", id, string(notification.BroadcastQueued)).
		Updates(map[string]interface{}{"status": string(notification.BroadcastCompleted), "workspace_count": workspaces, "completed_at": at}).Error
}

func (s *BroadcastStore) Broadcasts(ctx context.Context, before shared.Keyset, paged bool, limit int) ([]notification.Broadcast, error) {
	query := s.db.WithContext(ctx).Model(&schema.NotificationBroadcast{})
	if paged {
		if !validUUID(before.ID) {
			return nil, notification.ErrCursorInvalid
		}
		query = query.Where("(created_at, id) < (?, ?)", before.At, before.ID)
	}
	var rows []schema.NotificationBroadcast
	if err := query.Order("created_at DESC, id DESC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]notification.Broadcast, 0, len(rows))
	for _, row := range rows {
		b, err := broadcastOf(row)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, nil
}

func (s *BroadcastStore) StaleQueued(ctx context.Context, createdBefore time.Time, limit int) ([]string, error) {
	var ids []string
	err := s.db.WithContext(ctx).Model(&schema.NotificationBroadcast{}).
		Where("status = ? AND created_at < ?", string(notification.BroadcastQueued), createdBefore).
		Order("created_at").Limit(limit).Pluck("id", &ids).Error
	return ids, err
}

func broadcastRow(b notification.Broadcast) (schema.NotificationBroadcast, error) {
	target, err := json.Marshal(b.Target)
	if err != nil {
		return schema.NotificationBroadcast{}, err
	}
	audience, err := json.Marshal(b.Audience)
	if err != nil {
		return schema.NotificationBroadcast{}, err
	}
	return schema.NotificationBroadcast{
		ID: b.ID, Target: target, Audience: audience, Category: string(b.Category), Severity: string(b.Severity),
		Title: b.Title, Body: b.Body, ImageURL: b.ImageURL, ActionURL: b.ActionURL, ActionLabel: b.ActionLabel,
		Email: b.Email, CreatedBy: b.CreatedBy, Status: string(b.Status), WorkspaceCount: b.WorkspaceCount,
		CreatedAt: b.CreatedAt, CompletedAt: b.CompletedAt,
	}, nil
}

func broadcastOf(row schema.NotificationBroadcast) (notification.Broadcast, error) {
	var target notification.BroadcastTarget
	if err := json.Unmarshal(row.Target, &target); err != nil {
		return notification.Broadcast{}, err
	}
	var audience notification.Audience
	if err := json.Unmarshal(row.Audience, &audience); err != nil {
		return notification.Broadcast{}, err
	}
	return notification.Broadcast{
		BroadcastDraft: notification.BroadcastDraft{
			Target: target, Audience: audience, Category: notification.Category(row.Category), Severity: notification.Severity(row.Severity),
			Title: row.Title, Body: row.Body, ImageURL: row.ImageURL, ActionURL: row.ActionURL, ActionLabel: row.ActionLabel, Email: row.Email,
		},
		ID: row.ID, CreatedBy: row.CreatedBy, Status: notification.BroadcastStatus(row.Status), WorkspaceCount: row.WorkspaceCount,
		CreatedAt: row.CreatedAt.UTC(), CompletedAt: utcPtr(row.CompletedAt),
	}, nil
}
