package notification_repository

import (
	"context"
	"time"

	"vozko/domain/notification"
)

const (
	broadcastStatsSQL = "SELECT n.broadcast_id::text AS broadcast_id, count(*) AS recipients, count(r.read_at) AS read" +
		" FROM notification_recipients r JOIN notifications n ON n.id = r.notification_id" +
		" WHERE n.broadcast_id = ANY(?::uuid[]) GROUP BY n.broadcast_id"

	broadcastRecipientsSQL = "SELECT r.notification_id::text AS notification_id, r.user_id::text AS user_id, u.username AS name, u.email," +
		" r.workspace_id::text AS workspace_id, w.name AS workspace_name, r.read_at" +
		" FROM notification_recipients r JOIN notifications n ON n.id = r.notification_id" +
		" JOIN users u ON u.id = r.user_id JOIN workspaces w ON w.id = r.workspace_id" +
		" WHERE n.broadcast_id = ?"
	recipientsReadSQL   = " AND r.read_at IS NOT NULL"
	recipientsUnreadSQL = " AND r.read_at IS NULL"
	recipientsAfterSQL  = " AND (r.notification_id, r.user_id) > (?::uuid, ?::uuid)"
	recipientsOrderSQL  = " ORDER BY r.notification_id, r.user_id LIMIT ?"
)

func (s *BroadcastStore) BroadcastStats(ctx context.Context, broadcastIDs []string) (map[string]notification.BroadcastStats, error) {
	out := map[string]notification.BroadcastStats{}
	ids := validUUIDs(broadcastIDs)
	if len(ids) == 0 {
		return out, nil
	}
	var rows []struct {
		BroadcastID string
		Recipients  int
		Read        int
	}
	if err := s.db.WithContext(ctx).Raw(broadcastStatsSQL, ids).Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.BroadcastID] = notification.BroadcastStats{Recipients: r.Recipients, Read: r.Read}
	}
	return out, nil
}

func recipientsQuery(broadcastID string, q notification.RecipientQuery) (string, []interface{}) {
	query, args := broadcastRecipientsSQL, []interface{}{broadcastID}
	switch q.Status {
	case notification.ReadStatusRead:
		query += recipientsReadSQL
	case notification.ReadStatusUnread:
		query += recipientsUnreadSQL
	}
	if q.Paged {
		query += recipientsAfterSQL
		args = append(args, q.After.NotificationID, q.After.UserID)
	}
	return query + recipientsOrderSQL, append(args, q.Limit)
}

func (s *BroadcastStore) BroadcastRecipients(ctx context.Context, broadcastID string, q notification.RecipientQuery) ([]notification.BroadcastRecipient, error) {
	if !validUUID(broadcastID) {
		return nil, nil
	}
	if q.Paged && (!validUUID(q.After.NotificationID) || !validUUID(q.After.UserID)) {
		return nil, notification.ErrCursorInvalid
	}
	query, args := recipientsQuery(broadcastID, q)
	var rows []struct {
		NotificationID string
		UserID         string
		Name           string
		Email          string
		WorkspaceID    string
		WorkspaceName  string
		ReadAt         *time.Time
	}
	if err := s.db.WithContext(ctx).Raw(query, args...).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]notification.BroadcastRecipient, 0, len(rows))
	for _, r := range rows {
		out = append(out, notification.BroadcastRecipient{
			NotificationID: r.NotificationID, UserID: r.UserID, Name: r.Name, Email: r.Email,
			WorkspaceID: r.WorkspaceID, WorkspaceName: r.WorkspaceName, ReadAt: utcPtr(r.ReadAt),
		})
	}
	return out, nil
}
