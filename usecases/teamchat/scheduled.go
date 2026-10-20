package teamchat_usecase

import (
	"context"
	"errors"
	"log"
	"strings"
	"time"

	"vozko/domain/shared"
	"vozko/domain/teamchat"
)

func (s *Service) Schedule(ctx context.Context, by shared.Person, workspaceID, threadID string, d teamchat.Draft, sendAt time.Time) (teamchat.Scheduled, error) {
	st, err := s.enter(ctx, by, workspaceID, threadID)
	if err != nil {
		return teamchat.Scheduled{}, err
	}
	if err := s.recipientReady(st); err != nil {
		return teamchat.Scheduled{}, err
	}
	if err := s.checkReply(ctx, st.thread, d.ReplyToID); err != nil {
		return teamchat.Scheduled{}, err
	}
	d.Mentions = onlyMembers(st.roster, d.Mentions)
	scheduled, err := teamchat.NewScheduled(s.deps.NewID(), st.thread, by.UserID, d, sendAt, s.now())
	if err != nil {
		return teamchat.Scheduled{}, err
	}
	if err := s.deps.Chats.CreateScheduled(ctx, scheduled); err != nil {
		return teamchat.Scheduled{}, err
	}
	s.wake(scheduled)
	s.deps.Announcer.AnnounceTeamScheduled(workspaceID, by.UserID, scheduled)
	return scheduled, nil
}

func (s *Service) Scheduled(ctx context.Context, by shared.Person, workspaceID, threadID string) ([]teamchat.Scheduled, error) {
	st, err := s.enter(ctx, by, workspaceID, threadID)
	if err != nil {
		return nil, err
	}
	return s.deps.Chats.ListScheduled(ctx, st.thread.ID, by.UserID)
}

func (s *Service) Reschedule(ctx context.Context, by shared.Person, workspaceID, scheduledID string, change teamchat.Reschedule) (teamchat.Scheduled, error) {
	current, err := s.owned(ctx, by, workspaceID, scheduledID)
	if err != nil {
		return teamchat.Scheduled{}, err
	}
	changed, err := current.RescheduleBy(by.UserID, change.Body, change.SendAt, s.now())
	if err != nil {
		return teamchat.Scheduled{}, err
	}
	if err := s.deps.Chats.SaveScheduled(ctx, changed); err != nil {
		return teamchat.Scheduled{}, err
	}
	if change.SendAt != nil {
		s.wake(changed)
	}
	s.deps.Announcer.AnnounceTeamScheduled(workspaceID, by.UserID, changed)
	return changed, nil
}

func (s *Service) CancelScheduled(ctx context.Context, by shared.Person, workspaceID, scheduledID string) error {
	current, err := s.owned(ctx, by, workspaceID, scheduledID)
	if err != nil {
		return err
	}
	canceled, err := current.CancelBy(by.UserID, s.now())
	if err != nil {
		return err
	}
	if err := s.deps.Chats.SaveScheduled(ctx, canceled); err != nil {
		return err
	}
	s.deps.Announcer.AnnounceTeamScheduled(workspaceID, by.UserID, canceled)
	return nil
}

func (s *Service) owned(ctx context.Context, by shared.Person, workspaceID, scheduledID string) (teamchat.Scheduled, error) {
	if err := s.require(by, workspaceID); err != nil {
		return teamchat.Scheduled{}, err
	}
	current, err := s.deps.Chats.FindScheduled(ctx, strings.TrimSpace(scheduledID))
	if err != nil {
		return teamchat.Scheduled{}, err
	}
	if current.WorkspaceID != workspaceID || current.AuthorID != by.UserID {
		return teamchat.Scheduled{}, teamchat.ErrScheduledNotFound
	}
	return current, nil
}

func (s *Service) wake(scheduled teamchat.Scheduled) {
	if err := s.deps.Wake.ScheduleFire(scheduled.ID, scheduled.SendAt); err != nil {
		log.Printf("[team-chat] waking scheduled message %s failed, the minute sweep delivers it: %v", scheduled.ID, err)
	}
}

func (s *Service) DispatchScheduled(ctx context.Context, scheduledID string) error {
	scheduled, err := s.deps.Chats.FindScheduled(ctx, scheduledID)
	if errors.Is(err, teamchat.ErrScheduledNotFound) {
		log.Printf("[team-chat] scheduled message %s no longer exists", scheduledID)
		return nil
	}
	if err != nil {
		return err
	}
	if !scheduled.Due(s.now()) {
		return nil
	}
	author := shared.Person{UserID: scheduled.AuthorID}
	if !s.canUse(author.UserID, scheduled.WorkspaceID) {
		return s.fail(ctx, scheduled, teamchat.ReasonLeftWorkspace)
	}
	thread, err := s.deps.Chats.FindThread(ctx, scheduled.WorkspaceID, scheduled.ThreadID)
	if errors.Is(err, teamchat.ErrNotFound) {
		return s.fail(ctx, scheduled, teamchat.ReasonThreadUnavailable)
	}
	if err != nil {
		return err
	}
	roster, err := s.deps.Chats.Roster(ctx, thread.ID)
	if err != nil {
		return err
	}
	me, ok := roster.Active(scheduled.AuthorID)
	if !ok {
		return s.fail(ctx, scheduled, teamchat.ReasonNotMember)
	}
	sent, err := s.deliver(ctx, seat{thread: thread, roster: roster, me: me}, scheduled.Draft())
	switch {
	case errors.Is(err, teamchat.ErrRecipientUnavailable):
		return s.fail(ctx, scheduled, teamchat.ReasonRecipientUnavailable)
	case errors.Is(err, teamchat.ErrReplyOutsideThread):
		return s.fail(ctx, scheduled, teamchat.ReasonThreadUnavailable)
	case err != nil:
		return err
	}
	done, err := scheduled.Sent(sent.Message.ID, s.now())
	if err != nil {
		return nil
	}
	return s.settle(ctx, done)
}

func (s *Service) fail(ctx context.Context, scheduled teamchat.Scheduled, reason teamchat.FailureReason) error {
	failed, err := scheduled.Failed(reason, s.now())
	if err != nil {
		return nil
	}
	return s.settle(ctx, failed)
}

func (s *Service) settle(ctx context.Context, scheduled teamchat.Scheduled) error {
	err := s.deps.Chats.SaveScheduled(ctx, scheduled)
	if errors.Is(err, teamchat.ErrScheduledFinal) {
		return nil
	}
	if err != nil {
		return err
	}
	s.deps.Announcer.AnnounceTeamScheduled(scheduled.WorkspaceID, scheduled.AuthorID, scheduled)
	return nil
}

func (s *Service) SweepScheduled(ctx context.Context) error {
	due, err := s.deps.Chats.DueScheduled(ctx, s.now(), teamchat.ScheduledDueBatch)
	if err != nil {
		return err
	}
	if len(due) == teamchat.ScheduledDueBatch {
		log.Printf("[team-chat] the sweep hit its batch of %d; the rest goes out on the next tick", teamchat.ScheduledDueBatch)
	}
	for _, scheduled := range due {
		if err := s.DispatchScheduled(ctx, scheduled.ID); err != nil {
			log.Printf("[team-chat] sweep could not deliver %s: %v", scheduled.ID, err)
		}
	}
	return nil
}
