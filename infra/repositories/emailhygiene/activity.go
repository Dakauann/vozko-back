package emailhygiene_repository

import (
	"context"
	"time"

	"gorm.io/gorm"

	"vozko/domain/emailhygiene"
)

const (
	badAddressSQL = `INSERT INTO email_bad_addresses AS b (address_hash, reason, first_seen_at, last_seen_at)
VALUES (decode(?, 'hex'), 'hard_bounce', ?, ?)
ON CONFLICT (address_hash) DO UPDATE SET first_seen_at = LEAST(b.first_seen_at, EXCLUDED.first_seen_at), last_seen_at = GREATEST(b.last_seen_at, EXCLUDED.last_seen_at)`

	deliveredSQL = `INSERT INTO email_address_activity AS a (workspace_id, address_hash, first_delivered_at, last_delivered_at, sunset_since,
  campaigns_since_engaged, soft_streak, soft_streak_started_at, last_outcome_message_id, touched_at)
VALUES (?::uuid, decode(?, 'hex'), ?, ?, CASE WHEN ? THEN ?::timestamptz END, CASE WHEN ? THEN 1 ELSE 0 END, 0, NULL, ?, ?)
ON CONFLICT (workspace_id, address_hash) DO UPDATE SET
  first_delivered_at = LEAST(COALESCE(a.first_delivered_at, EXCLUDED.first_delivered_at), EXCLUDED.first_delivered_at),
  last_delivered_at = GREATEST(COALESCE(a.last_delivered_at, EXCLUDED.last_delivered_at), EXCLUDED.last_delivered_at),
  sunset_since = COALESCE(a.sunset_since, EXCLUDED.sunset_since),
  campaigns_since_engaged = a.campaigns_since_engaged + EXCLUDED.campaigns_since_engaged,
  soft_streak = 0, soft_streak_started_at = NULL,
  last_outcome_message_id = EXCLUDED.last_outcome_message_id, touched_at = GREATEST(a.touched_at, EXCLUDED.touched_at)
WHERE a.last_outcome_message_id IS DISTINCT FROM EXCLUDED.last_outcome_message_id`

	softBouncedSQL = `INSERT INTO email_address_activity AS a (workspace_id, address_hash, campaigns_since_engaged, soft_streak, soft_streak_started_at,
  last_outcome_message_id, touched_at)
VALUES (?::uuid, decode(?, 'hex'), 0, 1, ?, ?, ?)
ON CONFLICT (workspace_id, address_hash) DO UPDATE SET
  soft_streak = a.soft_streak + 1, soft_streak_started_at = COALESCE(a.soft_streak_started_at, EXCLUDED.soft_streak_started_at),
  last_outcome_message_id = EXCLUDED.last_outcome_message_id, touched_at = GREATEST(a.touched_at, EXCLUDED.touched_at)
WHERE a.last_outcome_message_id IS DISTINCT FROM EXCLUDED.last_outcome_message_id`

	streakSQL = `SELECT soft_streak, soft_streak_started_at FROM email_address_activity WHERE workspace_id = ?::uuid AND address_hash = decode(?, 'hex')`

	engagedSQL = `INSERT INTO email_address_activity AS a (workspace_id, address_hash, last_engaged_at, sunset_since, campaigns_since_engaged, soft_streak, touched_at)
VALUES (?::uuid, decode(?, 'hex'), ?, ?, 0, 0, ?)
ON CONFLICT (workspace_id, address_hash) DO UPDATE SET
  campaigns_since_engaged = CASE WHEN a.sunset_since IS NULL OR EXCLUDED.sunset_since >= a.sunset_since THEN 0 ELSE a.campaigns_since_engaged END,
  last_engaged_at = GREATEST(COALESCE(a.last_engaged_at, EXCLUDED.last_engaged_at), EXCLUDED.last_engaged_at),
  sunset_since = GREATEST(COALESCE(a.sunset_since, EXCLUDED.sunset_since), EXCLUDED.sunset_since),
  touched_at = GREATEST(a.touched_at, EXCLUDED.touched_at)`

	activityPruneSQL = `DELETE FROM email_address_activity WHERE ctid = ANY(ARRAY(
SELECT ctid FROM email_address_activity WHERE touched_at < ? ORDER BY touched_at LIMIT ?))`
)

type BadAddresses struct {
	db *gorm.DB
}

var _ emailhygiene.BadAddressStore = (*BadAddresses)(nil)

func NewBadAddresses(db *gorm.DB) *BadAddresses {
	return &BadAddresses{db: db}
}

func (s *BadAddresses) Add(ctx context.Context, hash emailhygiene.AddressHash, at time.Time) error {
	at = at.UTC()
	return s.db.WithContext(ctx).Exec(badAddressSQL, hash.Hex(), at, at).Error
}

type Activity struct {
	db *gorm.DB
}

var _ emailhygiene.ActivityStore = (*Activity)(nil)

func NewActivity(db *gorm.DB) *Activity {
	return &Activity{db: db}
}

func (s *Activity) Delivered(ctx context.Context, workspaceID string, hash emailhygiene.AddressHash, messageID string, campaign bool, at time.Time) error {
	at = at.UTC()
	return s.db.WithContext(ctx).Exec(deliveredSQL, workspaceID, hash.Hex(), at, at, campaign, at, campaign, messageID, at).Error
}

func (s *Activity) SoftBounced(ctx context.Context, workspaceID string, hash emailhygiene.AddressHash, messageID string, at time.Time) (emailhygiene.SoftStreak, error) {
	at = at.UTC()
	db := s.db.WithContext(ctx)
	if err := db.Exec(softBouncedSQL, workspaceID, hash.Hex(), at, messageID, at).Error; err != nil {
		return emailhygiene.SoftStreak{}, err
	}
	var row struct {
		SoftStreak          int
		SoftStreakStartedAt *time.Time
	}
	if err := db.Raw(streakSQL, workspaceID, hash.Hex()).Scan(&row).Error; err != nil {
		return emailhygiene.SoftStreak{}, err
	}
	streak := emailhygiene.SoftStreak{Count: row.SoftStreak}
	if row.SoftStreakStartedAt != nil {
		streak.StartedAt = *row.SoftStreakStartedAt
	}
	return streak, nil
}

func (s *Activity) Engaged(ctx context.Context, workspaceID string, hash emailhygiene.AddressHash, at time.Time) error {
	at = at.UTC()
	return s.db.WithContext(ctx).Exec(engagedSQL, workspaceID, hash.Hex(), at, at, at).Error
}

func (s *Activity) Prune(ctx context.Context, touchedBefore time.Time, limit int) (int64, error) {
	res := s.db.WithContext(ctx).Exec(activityPruneSQL, touchedBefore.UTC(), limit)
	return res.RowsAffected, res.Error
}
