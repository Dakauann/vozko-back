package notification_usecase

import (
	"context"
	"strings"
	"time"

	"vozko/domain/notification"
)

type InboxService struct {
	inbox       notification.Inbox
	preferences notification.PreferenceStore
	now         func() time.Time
}

func NewInboxService(inbox notification.Inbox, preferences notification.PreferenceStore, now func() time.Time) *InboxService {
	return &InboxService{inbox: inbox, preferences: preferences, now: clockOrNow(now)}
}

func (s *InboxService) List(ctx context.Context, v notification.Viewer, q notification.ListQuery) (notification.Page, error) {
	if err := v.Validate(); err != nil {
		return notification.Page{}, err
	}
	q = q.Normalize()
	probe := q
	probe.Limit = q.Limit + 1
	rows, err := s.inbox.List(ctx, v, probe)
	if err != nil {
		return notification.Page{}, err
	}
	return notification.PageOf(rows, q.Limit), nil
}

func (s *InboxService) Summary(ctx context.Context, v notification.Viewer) (notification.Summary, error) {
	if err := v.Validate(); err != nil {
		return notification.Summary{}, err
	}
	return s.inbox.Summary(ctx, v, notification.UnreadCountLimit)
}

func (s *InboxService) MarkRead(ctx context.Context, v notification.Viewer, ids []string) (int, error) {
	clean, err := s.change(v, ids)
	if err != nil {
		return 0, err
	}
	return s.inbox.MarkRead(ctx, v, clean, s.now())
}

func (s *InboxService) MarkUnread(ctx context.Context, v notification.Viewer, ids []string) (int, error) {
	clean, err := s.change(v, ids)
	if err != nil {
		return 0, err
	}
	return s.inbox.MarkUnread(ctx, v, clean)
}

func (s *InboxService) MarkAllRead(ctx context.Context, v notification.Viewer, category string) (int, error) {
	if err := v.Validate(); err != nil {
		return 0, err
	}
	c := notification.Category(strings.TrimSpace(category))
	if c != "" && !c.Valid() {
		return 0, notification.ErrCategoryUnknown
	}
	return s.inbox.MarkAllRead(ctx, v, c, s.now())
}

func (s *InboxService) Preferences(ctx context.Context, v notification.Viewer) (notification.Preferences, error) {
	if err := v.Validate(); err != nil {
		return notification.Preferences{}, err
	}
	return s.preferences.Preferences(ctx, v)
}

type PreferencesChange struct {
	EmailMuted  []string
	EmailDigest bool
}

func (s *InboxService) SavePreferences(ctx context.Context, v notification.Viewer, change PreferencesChange) (notification.Preferences, error) {
	if err := v.Validate(); err != nil {
		return notification.Preferences{}, err
	}
	muted, err := notification.MutedCategories(change.EmailMuted)
	if err != nil {
		return notification.Preferences{}, err
	}
	return s.preferences.SavePreferences(ctx, notification.Preferences{
		WorkspaceID: v.WorkspaceID, UserID: v.UserID, EmailMuted: muted, EmailDigest: change.EmailDigest, UpdatedAt: s.now(),
	})
}

func (s *InboxService) change(v notification.Viewer, ids []string) ([]string, error) {
	if err := v.Validate(); err != nil {
		return nil, err
	}
	return notification.ReadIDs(ids)
}
