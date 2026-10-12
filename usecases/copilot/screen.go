package copilot_usecase

import (
	"context"
	"errors"
	"fmt"

	"vozko/domain/copilot"
	"vozko/usecases/agentloop"
)

const (
	EventScreenCommand = "screen_command"
	EventScreenGo      = "screen_go"
)

type screenSession struct {
	threadID  string
	projectID string
	mailbox   copilot.ScreenMailbox
	emit      agentloop.Emit
	newID     IDGenerator
	settle    func(commandID string)
}

func (s *screenSession) Run(ctx context.Context, cmd copilot.ScreenCommand) (copilot.ScreenReply, error) {
	if !cmd.Name.Known() {
		return copilot.ScreenReply{}, fmt.Errorf("copilot: unknown screen command %q", cmd.Name)
	}
	if cmd.Name.NeedsProject() && s.projectID == "" {
		return copilot.ScreenReply{}, copilot.ErrNoScreen
	}
	cmd.ID = s.newID()
	cmd.ProjectID = s.projectID
	wait, cancel := s.mailbox.Expect(copilot.ScreenKey(s.threadID, cmd.ID))
	defer cancel()
	if s.settle != nil {
		defer s.settle(cmd.ID)
	}
	s.emit(EventScreenCommand, cmd)
	return s.awaitEditor(ctx, cmd, wait)
}

type screenWait func(context.Context) (copilot.ScreenReply, error)

func (s *screenSession) awaitEditor(parent context.Context, cmd copilot.ScreenCommand, wait screenWait) (copilot.ScreenReply, error) {
	ctx, stop := context.WithTimeout(parent, cmd.Name.Timeout())
	defer stop()
	reply, claimant, err := answerOrClaim(ctx, wait, cmd.Name.Commits())
	if err != nil || claimant == "" {
		return reply, err
	}
	s.emit(EventScreenGo, copilot.ScreenGo{ID: cmd.ID, Claimant: claimant})
	confirmed, cancel := context.WithTimeout(parent, cmd.Name.Timeout())
	defer cancel()
	reply, _, err = answerOrClaim(confirmed, wait, false)
	if errors.Is(err, copilot.ErrScreenUnavailable) || (err == nil && reply.Declined()) {
		return copilot.ScreenReply{}, copilot.ErrScreenUnconfirmed
	}
	return reply, err
}

func answerOrClaim(ctx context.Context, wait screenWait, claimable bool) (copilot.ScreenReply, string, error) {
	var declined *copilot.ScreenReply
	for {
		reply, err := wait(ctx)
		switch {
		case errors.Is(err, context.DeadlineExceeded) && declined != nil:
			return *declined, "", nil
		case errors.Is(err, context.DeadlineExceeded):
			return copilot.ScreenReply{}, "", copilot.ErrScreenUnavailable
		case err != nil:
			return reply, "", err
		case reply.Claim != "":
			if claimable {
				return copilot.ScreenReply{}, reply.Claim, nil
			}
		case reply.Declined():
			declined = &reply
		default:
			return reply, "", nil
		}
	}
}

func (s *Service) SetScreenMailbox(mailbox copilot.ScreenMailbox) {
	s.screens = mailbox
}

func (s *Service) DeliverScreenReply(threadID, commandID string, reply copilot.ScreenReply) error {
	if s.screens == nil {
		return copilot.ErrNoScreen
	}
	return s.screens.Deliver(copilot.ScreenKey(threadID, commandID), reply)
}

func (s *Service) ClaimScreenCommand(threadID, commandID, claimant string) error {
	claim, err := copilot.ScreenClaim(claimant)
	if err != nil {
		return err
	}
	if s.screens == nil {
		return copilot.ErrNoScreen
	}
	return s.screens.Deliver(copilot.ScreenKey(threadID, commandID), claim)
}

func (s *Service) attachScreen(cc copilot.Context, threadID string, emit agentloop.Emit) copilot.Context {
	cc.Screen = nil
	if s.screens == nil || s.newID == nil {
		return cc
	}
	projectID := ""
	if cc.View.OnStudio() {
		projectID = cc.View.ProjectID
	}
	cc.Screen = &screenSession{
		threadID:  threadID,
		projectID: projectID,
		mailbox:   s.screens,
		emit:      emit,
		newID:     s.newID,
		settle:    func(commandID string) { s.turns.Settle(threadID, commandID) },
	}
	return cc
}

func (s *Service) BeginTurn(parent context.Context, threadID, userID string) (*Turn, error) {
	return s.turns.Begin(parent, threadID, userID)
}

func (s *Service) ObserveTurn(threadID, userID string) (*Turn, error) {
	return s.turns.Observe(threadID, userID)
}

func (s *Service) StopTurn(threadID, userID string) error {
	return s.turns.Stop(threadID, userID)
}

func (s *Service) TurnRunning(threadID string) bool {
	return s.turns.Running(threadID)
}
