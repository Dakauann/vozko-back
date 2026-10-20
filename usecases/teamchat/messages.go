package teamchat_usecase

import (
	"context"
	"errors"
	"strings"

	"vozko/domain/shared"
	"vozko/domain/teamchat"
)

func (s *Service) Messages(ctx context.Context, by shared.Person, workspaceID string, q teamchat.MessageQuery) (teamchat.MessagePage, error) {
	st, err := s.enter(ctx, by, workspaceID, q.ThreadID)
	if err != nil {
		return teamchat.MessagePage{}, err
	}
	if err := q.Validate(); err != nil {
		return teamchat.MessagePage{}, err
	}
	size := teamchat.MessagePageLimit(q.Limit)
	q.ThreadID, q.Limit = st.thread.ID, size+1
	messages, err := s.deps.Chats.ListMessages(ctx, q)
	if err != nil {
		return teamchat.MessagePage{}, err
	}
	hasMore := len(messages) > size
	if hasMore {
		if q.AfterSeq > 0 {
			messages = messages[:size]
		} else {
			messages = messages[1:]
		}
	}
	return teamchat.MessagePage{Messages: s.authored(messages), HasMore: hasMore}, nil
}

func (s *Service) Send(ctx context.Context, by shared.Person, workspaceID, threadID string, d teamchat.Draft) (teamchat.Sent, error) {
	st, err := s.enter(ctx, by, workspaceID, threadID)
	if err != nil {
		return teamchat.Sent{}, err
	}
	return s.deliver(ctx, st, d)
}

func (s *Service) deliver(ctx context.Context, st seat, d teamchat.Draft) (teamchat.Sent, error) {
	if err := s.recipientReady(st); err != nil {
		return teamchat.Sent{}, err
	}
	if err := s.checkReply(ctx, st.thread, d.ReplyToID); err != nil {
		return teamchat.Sent{}, err
	}
	d.Mentions = onlyMembers(st.roster, d.Mentions)
	msg, err := teamchat.NewText(s.deps.NewID(), st.thread, st.me.UserID, d, s.now())
	if err != nil {
		return teamchat.Sent{}, err
	}
	stored, created, err := s.deps.Chats.Append(ctx, msg)
	if err != nil {
		return teamchat.Sent{}, err
	}
	authored := s.authoredOne(stored)
	if created {
		s.deps.Announcer.AnnounceTeamMessage(st.thread.WorkspaceID, st.activeIDs(), authored)
	}
	return teamchat.Sent{Message: authored, Created: created}, nil
}

func (s *Service) Edit(ctx context.Context, by shared.Person, workspaceID, messageID, body string) (teamchat.AuthoredMessage, error) {
	return s.change(ctx, by, workspaceID, messageID, func(m teamchat.Message) (teamchat.Message, error) {
		return m.EditBy(by.UserID, body, s.now())
	})
}

func (s *Service) Delete(ctx context.Context, by shared.Person, workspaceID, messageID string) (teamchat.AuthoredMessage, error) {
	return s.change(ctx, by, workspaceID, messageID, func(m teamchat.Message) (teamchat.Message, error) {
		return m.DeleteBy(by.UserID, s.now())
	})
}

func (s *Service) change(ctx context.Context, by shared.Person, workspaceID, messageID string, apply func(teamchat.Message) (teamchat.Message, error)) (teamchat.AuthoredMessage, error) {
	if err := s.require(by, workspaceID); err != nil {
		return teamchat.AuthoredMessage{}, err
	}
	msg, err := s.deps.Chats.FindMessage(ctx, workspaceID, strings.TrimSpace(messageID))
	if err != nil {
		return teamchat.AuthoredMessage{}, err
	}
	st, err := s.seat(ctx, by.UserID, workspaceID, msg.ThreadID)
	if errors.Is(err, teamchat.ErrNotFound) {
		return teamchat.AuthoredMessage{}, teamchat.ErrMessageNotFound
	}
	if err != nil {
		return teamchat.AuthoredMessage{}, err
	}
	changed, err := apply(msg)
	if err != nil {
		return teamchat.AuthoredMessage{}, err
	}
	if err := s.deps.Chats.SaveMessage(ctx, changed); err != nil {
		return teamchat.AuthoredMessage{}, err
	}
	authored := s.authoredOne(changed)
	s.deps.Announcer.AnnounceTeamMessageChanged(workspaceID, st.activeIDs(), authored)
	return authored, nil
}

func (s *Service) MarkRead(ctx context.Context, by shared.Person, workspaceID, threadID string, seq int64) (int64, error) {
	st, err := s.enter(ctx, by, workspaceID, threadID)
	if err != nil {
		return 0, err
	}
	if teamchat.ReadUpTo(st.me.LastReadSeq, seq, st.thread.LastSeq) == st.me.LastReadSeq {
		return st.me.LastReadSeq, nil
	}
	read, err := s.deps.Chats.MarkRead(ctx, st.thread.ID, by.UserID, seq)
	if err != nil {
		return 0, err
	}
	s.deps.Announcer.AnnounceTeamRead(workspaceID, st.activeIDs(), teamchat.ReadMark{ThreadID: st.thread.ID, UserID: by.UserID, Seq: read})
	return read, nil
}

func (s *Service) Typing(ctx context.Context, by shared.Person, workspaceID, threadID string) error {
	st, err := s.enter(ctx, by, workspaceID, threadID)
	if err != nil {
		return err
	}
	s.deps.Announcer.AnnounceTeamTyping(workspaceID, st.roster.OthersThan(by.UserID), st.thread.ID, by.UserID)
	return nil
}
