package teamchat_usecase

import (
	"context"
	"log"
	"sort"
	"strings"

	"vozko/domain/shared"
	"vozko/domain/teamchat"
)

const unknownName = "Alguém da equipe"

func (s *Service) people(ids []string) map[string]teamchat.Person {
	out := map[string]teamchat.Person{}
	ids = shared.DistinctTrimmed(ids)
	if len(ids) == 0 {
		return out
	}
	users, err := s.deps.People.FindByIDs(ids)
	if err != nil {
		log.Printf("[team-chat] resolving names failed: %v", err)
		return out
	}
	for _, u := range users {
		if u != nil {
			out[u.ID] = teamchat.Person{UserID: u.ID, Name: strings.TrimSpace(u.Username), Picture: u.Picture}
		}
	}
	return out
}

func personOf(people map[string]teamchat.Person, userID string) teamchat.Person {
	person := people[userID]
	person.UserID = userID
	if person.Name == "" {
		person.Name = unknownName
	}
	return person
}

func (s *Service) authored(messages []teamchat.Message) []teamchat.AuthoredMessage {
	ids := make([]string, 0, len(messages))
	for _, m := range messages {
		ids = append(ids, m.AuthorID)
	}
	people := s.people(ids)
	out := make([]teamchat.AuthoredMessage, 0, len(messages))
	for _, m := range messages {
		out = append(out, teamchat.AuthoredMessage{Message: m, AuthorName: personOf(people, m.AuthorID).Name})
	}
	return out
}

func (s *Service) authoredOne(m teamchat.Message) teamchat.AuthoredMessage {
	return s.authored([]teamchat.Message{m})[0]
}

func (s *Service) views(ctx context.Context, userID string, rows []teamchat.ThreadRow) ([]teamchat.ThreadView, error) {
	if len(rows) == 0 {
		return []teamchat.ThreadView{}, nil
	}
	threads := make([]teamchat.Thread, 0, len(rows))
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		threads = append(threads, row.Thread)
		ids = append(ids, row.Thread.ID)
	}
	rosters, err := s.deps.Chats.Rosters(ctx, ids)
	if err != nil {
		return nil, err
	}
	last, err := s.deps.Chats.LastMessages(ctx, threads)
	if err != nil {
		return nil, err
	}
	mentions, err := s.deps.Chats.UnreadMentions(ctx, userID, ids)
	if err != nil {
		return nil, err
	}
	personIDs := []string{}
	for _, roster := range rosters {
		personIDs = append(personIDs, roster.ActiveUserIDs()...)
	}
	for _, m := range last {
		personIDs = append(personIDs, m.AuthorID)
	}
	people := s.people(personIDs)

	out := make([]teamchat.ThreadView, 0, len(rows))
	for _, row := range rows {
		view := teamchat.ThreadView{
			Thread:         row.Thread,
			Me:             row.Me,
			Members:        participants(rosters[row.Thread.ID], people),
			Unread:         row.Me.Unread(row.Thread.LastSeq),
			UnreadMentions: mentions[row.Thread.ID],
		}
		if m, ok := last[row.Thread.ID]; ok {
			view.LastMessage = &teamchat.AuthoredMessage{Message: m, AuthorName: personOf(people, m.AuthorID).Name}
		}
		out = append(out, view)
	}
	return out, nil
}

func participants(roster teamchat.Roster, people map[string]teamchat.Person) []teamchat.Participant {
	out := []teamchat.Participant{}
	for _, m := range roster {
		if m.IsActive() {
			out = append(out, teamchat.Participant{Person: personOf(people, m.UserID), Role: m.Role, LastReadSeq: m.LastReadSeq})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out
}

func (s *Service) viewOf(ctx context.Context, userID, workspaceID, threadID string) (teamchat.ThreadView, error) {
	st, err := s.seat(ctx, userID, workspaceID, threadID)
	if err != nil {
		return teamchat.ThreadView{}, err
	}
	views, err := s.views(ctx, userID, []teamchat.ThreadRow{{Thread: st.thread, Me: st.me}})
	if err != nil {
		return teamchat.ThreadView{}, err
	}
	return views[0], nil
}
