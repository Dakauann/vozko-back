package teamchat_usecase

import (
	"context"
	"errors"
	"sort"
	"strings"

	"vozko/domain/shared"
	"vozko/domain/teamchat"
)

func (s *Service) Directory(ctx context.Context, by shared.Person, workspaceID string) ([]teamchat.Person, error) {
	if err := s.require(by, workspaceID); err != nil {
		return nil, err
	}
	members, err := s.deps.Members.ListMembers(workspaceID)
	if err != nil {
		return nil, err
	}
	usable := map[string]string{}
	for _, m := range members {
		if m != nil && m.UserID != "" && s.canUse(m.UserID, workspaceID) {
			usable[m.UserID] = strings.TrimSpace(m.Username)
		}
	}
	ids := make([]string, 0, len(usable))
	for id := range usable {
		ids = append(ids, id)
	}
	people := s.people(ids)
	out := make([]teamchat.Person, 0, len(ids))
	for _, id := range ids {
		person := people[id]
		person.UserID = id
		if person.Name == "" {
			person.Name = usable[id]
		}
		out = append(out, personOf(map[string]teamchat.Person{id: person}, id))
	}
	sort.Slice(out, func(i, j int) bool {
		left, right := strings.ToLower(out[i].Name), strings.ToLower(out[j].Name)
		if left == right {
			return out[i].UserID < out[j].UserID
		}
		return left < right
	})
	return out, nil
}

func (s *Service) Summary(ctx context.Context, by shared.Person, workspaceID string) (teamchat.Totals, error) {
	if err := s.require(by, workspaceID); err != nil {
		return teamchat.Totals{}, err
	}
	return s.deps.Chats.UnreadTotals(ctx, workspaceID, by.UserID)
}

func (s *Service) Threads(ctx context.Context, by shared.Person, workspaceID, cursor string, limit int) (teamchat.ThreadPage, error) {
	if err := s.require(by, workspaceID); err != nil {
		return teamchat.ThreadPage{}, err
	}
	after, hasCursor, err := shared.ParseKeyset(cursor)
	if err != nil {
		return teamchat.ThreadPage{}, err
	}
	size := teamchat.ThreadPageLimit(limit)
	q := teamchat.ThreadQuery{WorkspaceID: workspaceID, UserID: by.UserID, Limit: size + 1}
	if hasCursor {
		q.After = &after
	}
	rows, err := s.deps.Chats.ListThreads(ctx, q)
	if err != nil {
		return teamchat.ThreadPage{}, err
	}
	next := ""
	if len(rows) > size {
		rows = rows[:size]
		last := rows[size-1].Thread
		next = shared.Keyset{At: last.LastMessageAt, ID: last.ID}.Encode()
	}
	views, err := s.views(ctx, by.UserID, rows)
	if err != nil {
		return teamchat.ThreadPage{}, err
	}
	return teamchat.ThreadPage{Threads: views, Next: next}, nil
}

func (s *Service) Thread(ctx context.Context, by shared.Person, workspaceID, threadID string) (teamchat.ThreadView, error) {
	if err := s.require(by, workspaceID); err != nil {
		return teamchat.ThreadView{}, err
	}
	return s.viewOf(ctx, by.UserID, workspaceID, threadID)
}

func (s *Service) OpenDirect(ctx context.Context, by shared.Person, workspaceID, otherUserID string) (teamchat.ThreadView, error) {
	if err := s.require(by, workspaceID); err != nil {
		return teamchat.ThreadView{}, err
	}
	thread, members, err := teamchat.NewDirect(s.deps.NewID(), workspaceID, by.UserID, otherUserID, s.now())
	if err != nil {
		return teamchat.ThreadView{}, err
	}
	if !s.canUse(members[1].UserID, workspaceID) {
		return teamchat.ThreadView{}, teamchat.ErrMemberUnavailable
	}
	existing, err := s.deps.Chats.FindDirect(ctx, workspaceID, thread.DirectKey)
	switch {
	case err == nil:
		return s.viewOf(ctx, by.UserID, workspaceID, existing.ID)
	case !errors.Is(err, teamchat.ErrNotFound):
		return teamchat.ThreadView{}, err
	}
	stored, _, err := s.deps.Chats.CreateThread(ctx, thread, members, nil)
	if err != nil {
		return teamchat.ThreadView{}, err
	}
	return s.viewOf(ctx, by.UserID, workspaceID, stored.ID)
}

func (s *Service) CreateGroup(ctx context.Context, by shared.Person, workspaceID, title string, memberIDs []string) (teamchat.ThreadView, error) {
	if err := s.requireGroupCreation(by, workspaceID); err != nil {
		return teamchat.ThreadView{}, err
	}
	if err := s.allUsable(workspaceID, without(shared.DistinctTrimmed(memberIDs), by.UserID)); err != nil {
		return teamchat.ThreadView{}, err
	}
	now := s.now()
	thread, members, err := teamchat.NewGroup(s.deps.NewID(), workspaceID, by.UserID, title, memberIDs, now)
	if err != nil {
		return teamchat.ThreadView{}, err
	}
	notice := teamchat.NewSystem(s.deps.NewID(), thread, by.UserID, teamchat.EventCreated, nil, thread.Title, now)
	stored, _, err := s.deps.Chats.CreateThread(ctx, thread, members, &notice)
	if err != nil {
		return teamchat.ThreadView{}, err
	}
	others := []string{}
	for _, m := range members[1:] {
		others = append(others, m.UserID)
	}
	s.deps.Announcer.AnnounceTeamThread(workspaceID, others, stored.ID)
	return s.viewOf(ctx, by.UserID, workspaceID, stored.ID)
}

func (s *Service) allUsable(workspaceID string, ids []string) error {
	for _, id := range ids {
		if !s.canUse(id, workspaceID) {
			return teamchat.ErrMemberUnavailable
		}
	}
	return nil
}

func (s *Service) Rename(ctx context.Context, by shared.Person, workspaceID, threadID, title string) (teamchat.ThreadView, error) {
	st, err := s.managed(ctx, by, workspaceID, threadID)
	if err != nil {
		return teamchat.ThreadView{}, err
	}
	renamed, err := st.thread.Renamed(title)
	if err != nil {
		return teamchat.ThreadView{}, err
	}
	notice := teamchat.NewSystem(s.deps.NewID(), renamed, by.UserID, teamchat.EventRenamed, nil, renamed.Title, s.now())
	stored, err := s.deps.Chats.Rename(ctx, renamed, notice)
	if err != nil {
		return teamchat.ThreadView{}, err
	}
	s.announceChange(workspaceID, st.activeIDs(), stored)
	return s.viewOf(ctx, by.UserID, workspaceID, threadID)
}

func (s *Service) AddMembers(ctx context.Context, by shared.Person, workspaceID, threadID string, userIDs []string) (teamchat.ThreadView, error) {
	st, err := s.managed(ctx, by, workspaceID, threadID)
	if err != nil {
		return teamchat.ThreadView{}, err
	}
	if err := s.allUsable(workspaceID, shared.DistinctTrimmed(userIDs)); err != nil {
		return teamchat.ThreadView{}, err
	}
	now := s.now()
	joining, err := st.roster.Joining(st.thread, userIDs, now)
	if err != nil {
		return teamchat.ThreadView{}, err
	}
	added := make([]string, 0, len(joining))
	for _, m := range joining {
		added = append(added, m.UserID)
	}
	notice := teamchat.NewSystem(s.deps.NewID(), st.thread, by.UserID, teamchat.EventMembersAdded, added, "", now)
	stored, err := s.deps.Chats.ChangeMembers(ctx, st.thread, joining, notice)
	if err != nil {
		return teamchat.ThreadView{}, err
	}
	s.announceChange(workspaceID, append(st.activeIDs(), added...), stored)
	return s.viewOf(ctx, by.UserID, workspaceID, threadID)
}

func (s *Service) RemoveMember(ctx context.Context, by shared.Person, workspaceID, threadID, userID string) error {
	userID = strings.TrimSpace(userID)
	if userID == by.UserID {
		return s.Leave(ctx, by, workspaceID, threadID)
	}
	st, err := s.managed(ctx, by, workspaceID, threadID)
	if err != nil {
		return err
	}
	return s.depart(ctx, st, by.UserID, userID, teamchat.EventMemberRemoved)
}

func (s *Service) Leave(ctx context.Context, by shared.Person, workspaceID, threadID string) error {
	st, err := s.enter(ctx, by, workspaceID, threadID)
	if err != nil {
		return err
	}
	if !st.thread.IsGroup() {
		return teamchat.ErrDirectThread
	}
	return s.depart(ctx, st, by.UserID, by.UserID, teamchat.EventMemberLeft)
}

func (s *Service) depart(ctx context.Context, st seat, actorID, leaverID string, event teamchat.Event) error {
	now := s.now()
	changes, err := st.roster.Leaving(leaverID, now)
	if err != nil {
		return err
	}
	notice := teamchat.NewSystem(s.deps.NewID(), st.thread, actorID, event, []string{leaverID}, "", now)
	stored, err := s.deps.Chats.ChangeMembers(ctx, st.thread, changes, notice)
	if err != nil {
		return err
	}
	workspaceID := st.thread.WorkspaceID
	s.announceChange(workspaceID, without(st.activeIDs(), leaverID), stored)
	s.deps.Announcer.AnnounceTeamThreadRemoved(workspaceID, []string{leaverID}, st.thread.ID)
	return nil
}

func (s *Service) Mute(ctx context.Context, by shared.Person, workspaceID, threadID string, muted bool) error {
	st, err := s.enter(ctx, by, workspaceID, threadID)
	if err != nil {
		return err
	}
	if err := s.deps.Chats.SetMuted(ctx, st.thread.ID, by.UserID, muted); err != nil {
		return err
	}
	s.deps.Announcer.AnnounceTeamThread(workspaceID, []string{by.UserID}, st.thread.ID)
	return nil
}

func (s *Service) managed(ctx context.Context, by shared.Person, workspaceID, threadID string) (seat, error) {
	st, err := s.enter(ctx, by, workspaceID, threadID)
	if err != nil {
		return seat{}, err
	}
	if err := st.thread.ManagedBy(st.me); err != nil {
		return seat{}, err
	}
	return st, nil
}

func (s *Service) announceChange(workspaceID string, recipients []string, notice teamchat.Message) {
	s.deps.Announcer.AnnounceTeamMessage(workspaceID, recipients, s.authoredOne(notice))
	s.deps.Announcer.AnnounceTeamThread(workspaceID, recipients, notice.ThreadID)
}
