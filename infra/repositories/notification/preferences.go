package notification_repository

import (
	"context"
	"errors"

	"github.com/lib/pq"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"vozko/domain/notification"
	"vozko/infra/database/schema"
)

type PreferenceStore struct {
	db *gorm.DB
}

var _ notification.PreferenceStore = (*PreferenceStore)(nil)

func NewPreferenceStore(db *gorm.DB) *PreferenceStore {
	return &PreferenceStore{db: db}
}

func (s *PreferenceStore) Preferences(ctx context.Context, v notification.Viewer) (notification.Preferences, error) {
	empty := notification.Preferences{WorkspaceID: v.WorkspaceID, UserID: v.UserID, EmailMuted: []notification.Category{}}
	if !validViewer(v) {
		return empty, nil
	}
	var row schema.NotificationPreference
	err := s.db.WithContext(ctx).Where("workspace_id = ? AND user_id = ?", v.WorkspaceID, v.UserID).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return empty, nil
	}
	if err != nil {
		return notification.Preferences{}, err
	}
	return preferencesOf(row), nil
}

func (s *PreferenceStore) PreferencesFor(ctx context.Context, workspaceID string, userIDs []string) (map[string]notification.Preferences, error) {
	out := map[string]notification.Preferences{}
	ids := validUUIDs(userIDs)
	if !validUUID(workspaceID) || len(ids) == 0 {
		return out, nil
	}
	var rows []schema.NotificationPreference
	err := s.db.WithContext(ctx).
		Where("workspace_id = ? AND user_id = ANY(?::uuid[])", workspaceID, ids).
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		out[row.UserID] = preferencesOf(row)
	}
	return out, nil
}

func (s *PreferenceStore) SavePreferences(ctx context.Context, p notification.Preferences) (notification.Preferences, error) {
	if !validViewer(notification.Viewer{WorkspaceID: p.WorkspaceID, UserID: p.UserID}) {
		return notification.Preferences{}, notification.ErrViewerRequired
	}
	muted := make(pq.StringArray, 0, len(p.EmailMuted))
	for _, c := range p.EmailMuted {
		muted = append(muted, string(c))
	}
	row := schema.NotificationPreference{WorkspaceID: p.WorkspaceID, UserID: p.UserID, EmailMuted: muted, EmailDigest: p.EmailDigest, UpdatedAt: p.UpdatedAt}
	err := s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "workspace_id"}, {Name: "user_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"email_muted", "email_digest", "updated_at"}),
	}).Create(&row).Error
	if err != nil {
		return notification.Preferences{}, err
	}
	return preferencesOf(row), nil
}

func preferencesOf(row schema.NotificationPreference) notification.Preferences {
	muted := make([]notification.Category, 0, len(row.EmailMuted))
	for _, c := range row.EmailMuted {
		muted = append(muted, notification.Category(c))
	}
	return notification.Preferences{WorkspaceID: row.WorkspaceID, UserID: row.UserID, EmailMuted: muted, EmailDigest: row.EmailDigest, UpdatedAt: row.UpdatedAt.UTC()}
}
