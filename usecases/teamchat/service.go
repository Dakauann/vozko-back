package teamchat_usecase

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"vozko/domain/shared"
	"vozko/domain/teamchat"
	"vozko/domain/user"
	"vozko/domain/workspace"
)

type Access interface {
	HasWorkspacePermission(userID, workspaceID, resource, action string, isSystemAdmin bool) bool
	IsWorkspaceMember(userID, workspaceID string) bool
}

type Members interface {
	ListMembers(workspaceID string) ([]*workspace.Member, error)
}

type People interface {
	FindByIDs(ids []string) ([]*user.User, error)
}

type Announcer interface {
	AnnounceTeamMessage(workspaceID string, recipients []string, m teamchat.AuthoredMessage)
	AnnounceTeamMessageChanged(workspaceID string, recipients []string, m teamchat.AuthoredMessage)
	AnnounceTeamRead(workspaceID string, recipients []string, mark teamchat.ReadMark)
	AnnounceTeamThread(workspaceID string, recipients []string, threadID string)
	AnnounceTeamThreadRemoved(workspaceID string, recipients []string, threadID string)
	AnnounceTeamTyping(workspaceID string, recipients []string, threadID, userID string)
	AnnounceTeamScheduled(workspaceID, userID string, s teamchat.Scheduled)
}

type Deps struct {
	Chats     teamchat.Repository
	Access    Access
	Members   Members
	People    People
	Announcer Announcer
	Wake      teamchat.WakeScheduler
	Clock     shared.Clock
	NewID     func() string
}

type Service struct {
	deps Deps
}

var (
	_ teamchat.UseCases          = (*Service)(nil)
	_ teamchat.ScheduledDelivery = (*Service)(nil)
)

func New(deps Deps) (*Service, error) {
	missing := []string{}
	for name, absent := range map[string]bool{
		"chats":     deps.Chats == nil,
		"access":    deps.Access == nil,
		"members":   deps.Members == nil,
		"people":    deps.People == nil,
		"announcer": deps.Announcer == nil,
		"wake":      deps.Wake == nil,
	} {
		if absent {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		return nil, fmt.Errorf("team chat: missing %s", strings.Join(missing, ", "))
	}
	if deps.Clock == nil {
		deps.Clock = shared.SystemClock{}
	}
	if deps.NewID == nil {
		deps.NewID = uuid.NewString
	}
	return &Service{deps: deps}, nil
}

func (s *Service) now() time.Time {
	return s.deps.Clock.Now().UTC()
}

func (s *Service) canUse(userID, workspaceID string) bool {
	if strings.TrimSpace(userID) == "" || strings.TrimSpace(workspaceID) == "" {
		return false
	}
	return s.deps.Access.IsWorkspaceMember(userID, workspaceID)
}

func (s *Service) require(by shared.Person, workspaceID string) error {
	if !s.canUse(by.UserID, workspaceID) {
		return teamchat.ErrForbidden
	}
	return nil
}

func (s *Service) requireGroupCreation(by shared.Person, workspaceID string) error {
	if err := s.require(by, workspaceID); err != nil {
		return err
	}
	if !s.deps.Access.HasWorkspacePermission(by.UserID, workspaceID, string(workspace.ResourceTeamChat), string(workspace.ActionCreate), by.SystemAdmin) {
		return teamchat.ErrForbidden
	}
	return nil
}

type seat struct {
	thread teamchat.Thread
	roster teamchat.Roster
	me     teamchat.Member
}

func (st seat) activeIDs() []string {
	return st.roster.ActiveUserIDs()
}

func (s *Service) seat(ctx context.Context, userID, workspaceID, threadID string) (seat, error) {
	thread, err := s.deps.Chats.FindThread(ctx, workspaceID, strings.TrimSpace(threadID))
	if err != nil {
		return seat{}, err
	}
	roster, err := s.deps.Chats.Roster(ctx, thread.ID)
	if err != nil {
		return seat{}, err
	}
	me, ok := roster.Active(userID)
	if !ok {
		return seat{}, teamchat.ErrNotFound
	}
	return seat{thread: thread, roster: roster, me: me}, nil
}

func (s *Service) enter(ctx context.Context, by shared.Person, workspaceID, threadID string) (seat, error) {
	if err := s.require(by, workspaceID); err != nil {
		return seat{}, err
	}
	return s.seat(ctx, by.UserID, workspaceID, threadID)
}

func (s *Service) recipientReady(st seat) error {
	if st.thread.IsGroup() {
		return nil
	}
	other, ok := st.roster.Other(st.me.UserID)
	if !ok || !s.canUse(other.UserID, st.thread.WorkspaceID) {
		return teamchat.ErrRecipientUnavailable
	}
	return nil
}

func (s *Service) checkReply(ctx context.Context, thread teamchat.Thread, replyToID string) error {
	replyToID = strings.TrimSpace(replyToID)
	if replyToID == "" {
		return nil
	}
	quoted, err := s.deps.Chats.FindMessage(ctx, thread.WorkspaceID, replyToID)
	if errors.Is(err, teamchat.ErrMessageNotFound) || (err == nil && quoted.ThreadID != thread.ID) {
		return teamchat.ErrReplyOutsideThread
	}
	return err
}

func onlyMembers(roster teamchat.Roster, ids []string) []string {
	out := []string{}
	for _, id := range shared.DistinctTrimmed(ids) {
		if _, ok := roster.Active(id); ok {
			out = append(out, id)
		}
	}
	return out
}

func without(ids []string, person string) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if id != person {
			out = append(out, id)
		}
	}
	return out
}
