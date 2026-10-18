package notification_repository

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"vozko/domain/notification"
	"vozko/infra/database/schema"
)

const recipientBatch = 1000

const (
	stalePendingSQL = "SELECT id::text FROM notifications WHERE status = 'pending' AND created_at < ? ORDER BY created_at LIMIT ?"

	purgeSQL = "WITH doomed AS (SELECT id FROM notifications WHERE created_at < ? ORDER BY created_at LIMIT ?)," +
		" gone AS (DELETE FROM notification_recipients WHERE notification_id IN (SELECT id FROM doomed))" +
		" DELETE FROM notifications WHERE id IN (SELECT id FROM doomed)"

	inboxSelectSQL = "SELECT n.id::text AS id, n.category, n.severity, n.title, n.body, n.image_url, n.action_url, n.action_label," +
		" r.created_at, r.read_at FROM notification_recipients r JOIN notifications n ON n.id = r.notification_id" +
		" WHERE r.workspace_id = ? AND r.user_id = ?"
	inboxCategorySQL = " AND r.category = ?"
	inboxUnreadSQL   = " AND r.read_at IS NULL"
	inboxBeforeSQL   = " AND (r.created_at, r.notification_id) < (?, ?)"
	inboxOrderSQL    = " ORDER BY r.created_at DESC, r.notification_id DESC LIMIT ?"

	summarySQL = "SELECT category, count(*) AS unread FROM (SELECT category FROM notification_recipients" +
		" WHERE workspace_id = ? AND user_id = ? AND read_at IS NULL LIMIT ?) unread GROUP BY category"

	markReadSQL = "UPDATE notification_recipients SET read_at = ? WHERE workspace_id = ? AND user_id = ?" +
		" AND notification_id = ANY(?::uuid[]) AND read_at IS NULL"
	markUnreadSQL = "UPDATE notification_recipients SET read_at = NULL WHERE workspace_id = ? AND user_id = ?" +
		" AND notification_id = ANY(?::uuid[]) AND read_at IS NOT NULL"
	markAllReadSQL = "UPDATE notification_recipients SET read_at = ? WHERE workspace_id = ? AND user_id = ? AND read_at IS NULL"
)

type Store struct {
	db *gorm.DB
}

var (
	_ notification.Outbox = (*Store)(nil)
	_ notification.Inbox  = (*Store)(nil)
)

func NewStore(db *gorm.DB) *Store {
	return &Store{db: db}
}

func (s *Store) Create(ctx context.Context, r notification.Record) (notification.Record, bool, error) {
	row, err := notificationRow(r)
	if err != nil {
		return notification.Record{}, false, err
	}
	result := s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:     []clause.Column{{Name: "workspace_id"}, {Name: "dedup_key"}},
		TargetWhere: clause.Where{Exprs: []clause.Expression{clause.Expr{SQL: "dedup_key IS NOT NULL"}}},
		DoNothing:   true,
	}).Create(&row)
	if result.Error != nil {
		return notification.Record{}, false, result.Error
	}
	if result.RowsAffected == 1 {
		created, err := recordOf(row)
		return created, true, err
	}
	var existing schema.Notification
	if err := s.db.WithContext(ctx).Where("workspace_id = ? AND dedup_key = ?", row.WorkspaceID, row.DedupKey).Take(&existing).Error; err != nil {
		return notification.Record{}, false, err
	}
	found, err := recordOf(existing)
	return found, false, err
}

func (s *Store) Get(ctx context.Context, id string) (notification.Record, error) {
	if !validUUID(id) {
		return notification.Record{}, notification.ErrNotFound
	}
	var row schema.Notification
	err := s.db.WithContext(ctx).Where("id = ?", id).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return notification.Record{}, notification.ErrNotFound
	}
	if err != nil {
		return notification.Record{}, err
	}
	return recordOf(row)
}

func (s *Store) AddRecipients(ctx context.Context, recipients []notification.Recipient) error {
	if len(recipients) == 0 {
		return nil
	}
	rows := make([]schema.NotificationRecipient, 0, len(recipients))
	for _, r := range recipients {
		rows = append(rows, schema.NotificationRecipient{
			NotificationID: r.NotificationID, UserID: r.UserID, WorkspaceID: r.WorkspaceID,
			Category: string(r.Category), CreatedAt: r.CreatedAt,
		})
	}
	return s.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).CreateInBatches(&rows, recipientBatch).Error
}

func (s *Store) MarkDelivered(ctx context.Context, id string, recipients int, at time.Time) error {
	return s.db.WithContext(ctx).Model(&schema.Notification{}).
		Where("id = ? AND status = ?", id, string(notification.StatusPending)).
		Updates(map[string]interface{}{"status": string(notification.StatusDelivered), "recipient_count": recipients, "delivered_at": at}).Error
}

func (s *Store) StalePending(ctx context.Context, createdBefore time.Time, limit int) ([]string, error) {
	var ids []string
	err := s.db.WithContext(ctx).Raw(stalePendingSQL, createdBefore, limit).Scan(&ids).Error
	return ids, err
}

func (s *Store) PurgeBefore(ctx context.Context, cutoff time.Time, limit int) (int, error) {
	result := s.db.WithContext(ctx).Exec(purgeSQL, cutoff, limit)
	return int(result.RowsAffected), result.Error
}

type ItemRow struct {
	ID          string
	Category    string
	Severity    string
	Title       string
	Body        string
	ImageURL    string
	ActionURL   string
	ActionLabel string
	CreatedAt   time.Time
	ReadAt      *time.Time
}

func (r ItemRow) item() notification.Item {
	return notification.Item{
		ID: r.ID, Category: notification.Category(r.Category), Severity: notification.Severity(r.Severity),
		Title: r.Title, Body: r.Body, ImageURL: r.ImageURL, ActionURL: r.ActionURL, ActionLabel: r.ActionLabel,
		CreatedAt: r.CreatedAt.UTC(), ReadAt: utcPtr(r.ReadAt),
	}
}

func inboxQuery(v notification.Viewer, q notification.ListQuery) (string, []interface{}) {
	query := inboxSelectSQL
	args := []interface{}{v.WorkspaceID, v.UserID}
	if q.Category != "" {
		query += inboxCategorySQL
		args = append(args, string(q.Category))
	}
	if q.UnreadOnly {
		query += inboxUnreadSQL
	}
	if q.Paged {
		query += inboxBeforeSQL
		args = append(args, q.Before.At, q.Before.ID)
	}
	return query + inboxOrderSQL, append(args, q.Limit)
}

func (s *Store) List(ctx context.Context, v notification.Viewer, q notification.ListQuery) ([]notification.Item, error) {
	if !validViewer(v) {
		return nil, nil
	}
	if q.Paged && !validUUID(q.Before.ID) {
		return nil, notification.ErrCursorInvalid
	}
	query, args := inboxQuery(v, q)
	var rows []ItemRow
	if err := s.db.WithContext(ctx).Raw(query, args...).Scan(&rows).Error; err != nil {
		return nil, err
	}
	items := make([]notification.Item, 0, len(rows))
	for _, r := range rows {
		items = append(items, r.item())
	}
	return items, nil
}

func (s *Store) Summary(ctx context.Context, v notification.Viewer, scanLimit int) (notification.Summary, error) {
	summary := notification.Summary{ByCategory: map[notification.Category]int{}}
	if !validViewer(v) {
		return summary, nil
	}
	var rows []struct {
		Category string
		Unread   int
	}
	if err := s.db.WithContext(ctx).Raw(summarySQL, v.WorkspaceID, v.UserID, scanLimit).Scan(&rows).Error; err != nil {
		return notification.Summary{}, err
	}
	for _, r := range rows {
		summary.ByCategory[notification.Category(r.Category)] = r.Unread
		summary.Unread += r.Unread
	}
	summary.Capped = summary.Unread >= scanLimit
	return summary, nil
}

func (s *Store) MarkRead(ctx context.Context, v notification.Viewer, ids []string, at time.Time) (int, error) {
	return s.change(ctx, v, ids, markReadSQL, at)
}

func (s *Store) MarkUnread(ctx context.Context, v notification.Viewer, ids []string) (int, error) {
	return s.change(ctx, v, ids, markUnreadSQL)
}

func (s *Store) change(ctx context.Context, v notification.Viewer, ids []string, statement string, leading ...interface{}) (int, error) {
	valid := validUUIDs(ids)
	if !validViewer(v) || len(valid) == 0 {
		return 0, nil
	}
	args := append(leading, v.WorkspaceID, v.UserID, valid)
	result := s.db.WithContext(ctx).Exec(statement, args...)
	return int(result.RowsAffected), result.Error
}

func (s *Store) MarkAllRead(ctx context.Context, v notification.Viewer, category notification.Category, at time.Time) (int, error) {
	if !validViewer(v) {
		return 0, nil
	}
	statement, args := markAllReadSQL, []interface{}{at, v.WorkspaceID, v.UserID}
	if category != "" {
		statement += " AND category = ?"
		args = append(args, string(category))
	}
	result := s.db.WithContext(ctx).Exec(statement, args...)
	return int(result.RowsAffected), result.Error
}

func notificationRow(r notification.Record) (schema.Notification, error) {
	audience, err := json.Marshal(r.Audience)
	if err != nil {
		return schema.Notification{}, err
	}
	var emailCopy datatypes.JSON
	if r.EmailCopy != nil {
		if emailCopy, err = json.Marshal(r.EmailCopy); err != nil {
			return schema.Notification{}, err
		}
	}
	return schema.Notification{
		ID: r.ID, WorkspaceID: r.WorkspaceID, Category: string(r.Category), Severity: string(r.Severity),
		Title: r.Title, Body: r.Body, ImageURL: r.ImageURL, ActionURL: r.ActionURL, ActionLabel: r.ActionLabel,
		Audience: audience, Email: r.Email, EmailCopy: emailCopy, DedupKey: optional(r.DedupKey), Source: r.Source,
		BroadcastID: optional(r.BroadcastID), CreatedBy: optional(r.CreatedBy), Status: string(r.Status),
		RecipientCount: r.RecipientCount, CreatedAt: r.CreatedAt, DeliveredAt: r.DeliveredAt,
	}, nil
}

func recordOf(row schema.Notification) (notification.Record, error) {
	var audience notification.Audience
	if err := json.Unmarshal(row.Audience, &audience); err != nil {
		return notification.Record{}, err
	}
	var emailCopy *notification.EmailCopy
	if len(row.EmailCopy) > 0 && string(row.EmailCopy) != "null" {
		emailCopy = &notification.EmailCopy{}
		if err := json.Unmarshal(row.EmailCopy, emailCopy); err != nil {
			return notification.Record{}, err
		}
	}
	return notification.Record{
		Draft: notification.Draft{
			WorkspaceID: row.WorkspaceID, Category: notification.Category(row.Category), Severity: notification.Severity(row.Severity),
			Title: row.Title, Body: row.Body, ImageURL: row.ImageURL, ActionURL: row.ActionURL, ActionLabel: row.ActionLabel,
			Audience: audience, Email: row.Email, EmailCopy: emailCopy, DedupKey: deref(row.DedupKey), Source: row.Source,
			BroadcastID: deref(row.BroadcastID), CreatedBy: deref(row.CreatedBy),
		},
		ID: row.ID, Status: notification.Status(row.Status), RecipientCount: row.RecipientCount,
		CreatedAt: row.CreatedAt.UTC(), DeliveredAt: utcPtr(row.DeliveredAt),
	}, nil
}

func validUUID(id string) bool {
	_, err := uuid.Parse(strings.TrimSpace(id))
	return err == nil
}

func validUUIDs(ids []string) pq.StringArray {
	out := make(pq.StringArray, 0, len(ids))
	for _, id := range ids {
		if validUUID(id) {
			out = append(out, strings.TrimSpace(id))
		}
	}
	return out
}

func validViewer(v notification.Viewer) bool {
	return validUUID(v.WorkspaceID) && validUUID(v.UserID)
}

func optional(v string) *string {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	return &v
}

func deref(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func utcPtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}
