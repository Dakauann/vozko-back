package personalinbox_usecase

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"vozko/domain/conversation"
	"vozko/domain/personalinbox"
	"vozko/domain/shared"
	"vozko/domain/workspace"
)

type Access interface {
	HasWorkspacePermission(userID, workspaceID, resource, action string, isSystemAdmin bool) bool
	CanAccessEntry(userID, workspaceID, entryID, entryType string, isAdmin bool) bool
}

type Announcer interface {
	AnnouncePersonalMark(workspaceID string, mark personalinbox.Mark)
}

type Deps struct {
	Marks     personalinbox.Repository
	Access    Access
	Announcer Announcer
	Now       func() time.Time
}

type Service struct {
	deps Deps
}

func New(deps Deps) (*Service, error) {
	missing := []string{}
	if deps.Marks == nil {
		missing = append(missing, "marks")
	}
	if deps.Access == nil {
		missing = append(missing, "access")
	}
	if deps.Announcer == nil {
		missing = append(missing, "announcer")
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("personal inbox: missing %s", strings.Join(missing, ", "))
	}
	if deps.Now == nil {
		deps.Now = func() time.Time { return time.Now().UTC() }
	}
	return &Service{deps: deps}, nil
}

func (s *Service) Pin(ctx context.Context, by shared.Person, workspaceID string, entry shared.EntryRef, pinned bool) (personalinbox.Mark, error) {
	now := s.deps.Now()
	return s.write(ctx, by, workspaceID, entry, func(m personalinbox.Mark) (personalinbox.Mark, error) {
		return m.SetPinned(pinned, now), nil
	})
}

func (s *Service) MarkUnread(ctx context.Context, by shared.Person, workspaceID string, entry shared.EntryRef) (personalinbox.Mark, error) {
	now := s.deps.Now()
	return s.write(ctx, by, workspaceID, entry, func(m personalinbox.Mark) (personalinbox.Mark, error) {
		return m.MarkUnread(now), nil
	})
}

func (s *Service) ClearUnreadMark(ctx context.Context, by shared.Person, workspaceID string, entry shared.EntryRef) (personalinbox.Mark, error) {
	return s.write(ctx, by, workspaceID, entry, func(m personalinbox.Mark) (personalinbox.Mark, error) {
		return m.MarkRead(), nil
	})
}

func (s *Service) MarkRead(ctx context.Context, by shared.Person, workspaceID string, entry shared.EntryRef) {
	current, err := s.deps.Marks.Find(ctx, by.UserID, entry)
	if err != nil || !current.Unread() || !s.visible(by, workspaceID, entry) {
		return
	}
	before, after, err := s.deps.Marks.Change(ctx, by.UserID, entry, func(m personalinbox.Mark) (personalinbox.Mark, error) {
		return m.MarkRead(), nil
	})
	if err != nil {
		log.Printf("[personal-inbox] clearing the unread mark of %s on %s:%s failed: %v", by.UserID, entry.EntryType, entry.EntryID, err)
		return
	}
	if !before.Same(after) {
		s.deps.Announcer.AnnouncePersonalMark(workspaceID, after)
	}
}

func (s *Service) PageMarks(ctx context.Context, userID string, entries []shared.EntryRef) []personalinbox.Mark {
	if len(entries) == 0 {
		return nil
	}
	marks, err := s.deps.Marks.ForEntries(ctx, userID, entries)
	if err != nil {
		log.Printf("[personal-inbox] reading the marks of %s failed: %v", userID, err)
		return nil
	}
	return marks
}

func (s *Service) write(ctx context.Context, by shared.Person, workspaceID string, entry shared.EntryRef, change personalinbox.Change) (personalinbox.Mark, error) {
	if !s.visible(by, workspaceID, entry) {
		return personalinbox.Mark{}, conversation.ErrUnauthorized
	}
	_, after, err := s.deps.Marks.Change(ctx, by.UserID, entry, change)
	if err != nil {
		return personalinbox.Mark{}, err
	}
	s.deps.Announcer.AnnouncePersonalMark(workspaceID, after)
	return after, nil
}

func (s *Service) visible(by shared.Person, workspaceID string, entry shared.EntryRef) bool {
	if workspaceID == "" || strings.TrimSpace(entry.EntryID) == "" {
		return false
	}
	if !s.deps.Access.HasWorkspacePermission(by.UserID, workspaceID, string(workspace.ResourceConversations), string(workspace.ActionRead), by.SystemAdmin) {
		return false
	}
	return by.MayActOn(s.deps.Access, workspaceID, entry.EntryID, string(entry.EntryType))
}

var _ personalinbox.UseCases = (*Service)(nil)
