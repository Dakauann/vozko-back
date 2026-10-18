package notification_repository

import (
	"context"
	"time"

	"github.com/lib/pq"

	"vozko/domain/notification"
)

const (
	holdForDigestSQL = "UPDATE notification_recipients SET digest_pending = true WHERE notification_id = ? AND user_id = ANY(?::uuid[])"

	heldWorkspacesSQL      = "SELECT DISTINCT workspace_id::text FROM notification_recipients WHERE digest_pending AND created_at <= ? ORDER BY 1 LIMIT ?"
	heldWorkspacesAfterSQL = "SELECT DISTINCT workspace_id::text FROM notification_recipients WHERE digest_pending AND created_at <= ? AND workspace_id > ?::uuid ORDER BY 1 LIMIT ?"

	heldDigestsSQL = "SELECT user_id, id, category, severity, title, body, image_url, action_url, action_label, created_at, total FROM (" +
		"SELECT r.user_id::text AS user_id, n.id::text AS id, n.category, n.severity, n.title, n.body, n.image_url, n.action_url, n.action_label, r.created_at," +
		" count(*) OVER (PARTITION BY r.user_id) AS total," +
		" row_number() OVER (PARTITION BY r.user_id ORDER BY r.created_at DESC, r.notification_id DESC) AS rank" +
		" FROM notification_recipients r JOIN notifications n ON n.id = r.notification_id" +
		" WHERE r.workspace_id = ? AND r.digest_pending AND r.read_at IS NULL AND r.created_at <= ?" +
		") held WHERE rank <= ? ORDER BY user_id, created_at DESC, id DESC"

	releaseDigestsSQL = "UPDATE notification_recipients SET digest_pending = false WHERE workspace_id = ? AND digest_pending AND created_at <= ?" +
		" AND NOT (user_id = ANY(?::uuid[]))"
)

var _ notification.DigestStore = (*Store)(nil)

func (s *Store) HoldForDigest(ctx context.Context, notificationID string, userIDs []string) error {
	ids := validUUIDs(userIDs)
	if !validUUID(notificationID) || len(ids) == 0 {
		return nil
	}
	return s.db.WithContext(ctx).Exec(holdForDigestSQL, notificationID, ids).Error
}

func (s *Store) WorkspacesWithHeldDigests(ctx context.Context, upTo time.Time, afterID string, limit int) ([]string, error) {
	var ids []string
	query := s.db.WithContext(ctx)
	var err error
	if afterID == "" {
		err = query.Raw(heldWorkspacesSQL, upTo, limit).Scan(&ids).Error
	} else if validUUID(afterID) {
		err = query.Raw(heldWorkspacesAfterSQL, upTo, afterID, limit).Scan(&ids).Error
	}
	return ids, err
}

type heldRow struct {
	UserID string
	ItemRow
	Total int
}

func (s *Store) HeldDigests(ctx context.Context, workspaceID string, upTo time.Time, itemLimit int) ([]notification.Digest, error) {
	if !validUUID(workspaceID) {
		return nil, nil
	}
	var rows []heldRow
	if err := s.db.WithContext(ctx).Raw(heldDigestsSQL, workspaceID, upTo, itemLimit).Scan(&rows).Error; err != nil {
		return nil, err
	}
	var digests []notification.Digest
	for _, row := range rows {
		if len(digests) == 0 || digests[len(digests)-1].UserID != row.UserID {
			digests = append(digests, notification.Digest{WorkspaceID: workspaceID, UserID: row.UserID, Total: row.Total})
		}
		last := &digests[len(digests)-1]
		last.Items = append(last.Items, row.item())
	}
	return digests, nil
}

func (s *Store) ReleaseDigests(ctx context.Context, workspaceID string, upTo time.Time, keepUserIDs []string) error {
	if !validUUID(workspaceID) {
		return nil
	}
	keep := validUUIDs(keepUserIDs)
	if keep == nil {
		keep = pq.StringArray{}
	}
	return s.db.WithContext(ctx).Exec(releaseDigestsSQL, workspaceID, upTo, keep).Error
}
