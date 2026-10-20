package notification_repository

import (
	"context"
	"errors"
	"time"

	"github.com/lib/pq"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"vozko/domain/notification"
	"vozko/infra/database/schema"
)

const (
	seeEpisodeSQL = "UPDATE notification_episodes SET last_seen_at = GREATEST(last_seen_at, ?) WHERE id = ? AND closed_at IS NULL"

	advanceEpisodeSQL = "UPDATE notification_episodes SET steps_sent = ?, notification_id = ?, last_sent_at = ?" +
		" WHERE id = ? AND steps_sent = ? AND closed_at IS NULL"

	closeEpisodeSQL = "UPDATE notification_episodes SET closed_at = ?, close_reason = ? WHERE id = ? AND closed_at IS NULL"

	resolveEpisodesSQL = "UPDATE notification_episodes SET closed_at = ?, close_reason = ?" +
		" WHERE workspace_id = ? AND condition_key = ANY(?::text[]) AND closed_at IS NULL"

	purgeEpisodesSQL = "DELETE FROM notification_episodes WHERE id IN (SELECT id FROM notification_episodes" +
		" WHERE COALESCE(closed_at, last_seen_at) < ? LIMIT ?)"
)

type EpisodeStore struct {
	db *gorm.DB
}

var _ notification.EpisodeStore = (*EpisodeStore)(nil)

func NewEpisodeStore(db *gorm.DB) *EpisodeStore {
	return &EpisodeStore{db: db}
}

func (s *EpisodeStore) OpenEpisode(ctx context.Context, workspaceID, key string) (notification.Episode, bool, error) {
	if !validUUID(workspaceID) {
		return notification.Episode{}, false, notification.ErrWorkspaceRequired
	}
	var row schema.NotificationEpisode
	err := s.db.WithContext(ctx).Where("workspace_id = ? AND condition_key = ? AND closed_at IS NULL", workspaceID, key).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return notification.Episode{}, false, nil
	}
	if err != nil {
		return notification.Episode{}, false, err
	}
	return episodeOf(row), true, nil
}

func (s *EpisodeStore) StartEpisode(ctx context.Context, e notification.Episode) (notification.Episode, error) {
	if !validUUID(e.WorkspaceID) {
		return notification.Episode{}, notification.ErrWorkspaceRequired
	}
	row := schema.NotificationEpisode{WorkspaceID: e.WorkspaceID, ConditionKey: e.Key, OpenedAt: e.OpenedAt.UTC(), LastSeenAt: e.LastSeenAt.UTC()}
	err := s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:     []clause.Column{{Name: "workspace_id"}, {Name: "condition_key"}},
		TargetWhere: clause.Where{Exprs: []clause.Expression{clause.Expr{SQL: "closed_at IS NULL"}}},
		DoNothing:   true,
	}).Create(&row).Error
	if err != nil {
		return notification.Episode{}, err
	}
	open, found, err := s.OpenEpisode(ctx, e.WorkspaceID, e.Key)
	if err != nil {
		return notification.Episode{}, err
	}
	if !found {
		return notification.Episode{}, errors.New("notification episode: closed while it was being opened")
	}
	return open, nil
}

func (s *EpisodeStore) SeeEpisode(ctx context.Context, id string, at time.Time) error {
	if !validUUID(id) {
		return notification.ErrNotFound
	}
	return s.db.WithContext(ctx).Exec(seeEpisodeSQL, at.UTC(), id).Error
}

func (s *EpisodeStore) AdvanceEpisode(ctx context.Context, id string, step notification.EpisodeStep, notificationID string, at time.Time) (bool, error) {
	if !validUUID(id) || !validUUID(notificationID) {
		return false, notification.ErrNotFound
	}
	result := s.db.WithContext(ctx).Exec(advanceEpisodeSQL, int(step), notificationID, at.UTC(), id, int(step)-1)
	return result.RowsAffected == 1, result.Error
}

func (s *EpisodeStore) CloseEpisode(ctx context.Context, id string, at time.Time, reason notification.EpisodeClose) error {
	if !validUUID(id) {
		return notification.ErrNotFound
	}
	return s.db.WithContext(ctx).Exec(closeEpisodeSQL, at.UTC(), string(reason), id).Error
}

func (s *EpisodeStore) ResolveEpisodes(ctx context.Context, workspaceID string, keys []string, at time.Time) (int, error) {
	if !validUUID(workspaceID) {
		return 0, notification.ErrWorkspaceRequired
	}
	if len(keys) == 0 {
		return 0, nil
	}
	result := s.db.WithContext(ctx).Exec(resolveEpisodesSQL, at.UTC(), string(notification.EpisodeResolved), workspaceID, pq.StringArray(keys))
	return int(result.RowsAffected), result.Error
}

func (s *EpisodeStore) PurgeEpisodesBefore(ctx context.Context, cutoff time.Time, limit int) (int, error) {
	result := s.db.WithContext(ctx).Exec(purgeEpisodesSQL, cutoff.UTC(), limit)
	return int(result.RowsAffected), result.Error
}

func episodeOf(row schema.NotificationEpisode) notification.Episode {
	return notification.Episode{
		ID:             row.ID,
		WorkspaceID:    row.WorkspaceID,
		Key:            row.ConditionKey,
		StepsSent:      notification.EpisodeStep(row.StepsSent),
		OpenedAt:       row.OpenedAt.UTC(),
		LastSeenAt:     row.LastSeenAt.UTC(),
		LastSentAt:     utcPtr(row.LastSentAt),
		NotificationID: deref(row.NotificationID),
		ClosedAt:       utcPtr(row.ClosedAt),
		CloseReason:    notification.EpisodeClose(row.CloseReason),
	}
}
