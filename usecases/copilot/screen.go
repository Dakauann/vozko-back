package copilot_usecase

import (
	"context"
	"errors"
	"fmt"

	"vozko/domain/copilot"
	"vozko/usecases/agentloop"
)

const EventScreenCommand = "screen_command"

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
	cmd.ID = s.newID()
	cmd.ProjectID = s.projectID
	wait, cancel := s.mailbox.Expect(copilot.ScreenKey(s.threadID, cmd.ID))
	defer cancel()
	if s.settle != nil {
		defer s.settle(cmd.ID)
	}
	s.emit(EventScreenCommand, cmd)
	ctx, stop := context.WithTimeout(ctx, cmd.Name.Timeout())
	defer stop()
	return awaitEditor(ctx, wait)
}

func awaitEditor(ctx context.Context, wait func(context.Context) (copilot.ScreenReply, error)) (copilot.ScreenReply, error) {
	var declined *copilot.ScreenReply
	for {
		reply, err := wait(ctx)
		switch {
		case errors.Is(err, context.DeadlineExceeded) && declined != nil:
			return *declined, nil
		case errors.Is(err, context.DeadlineExceeded):
			return copilot.ScreenReply{}, copilot.ErrScreenUnavailable
		case err != nil:
			return reply, err
		case reply.Declined():
			declined = &reply
		default:
			return reply, nil
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

func (s *Service) attachScreen(cc copilot.Context, threadID string, emit agentloop.Emit) copilot.Context {
	cc.Screen = nil
	if !cc.View.OnStudio() || s.screens == nil || s.newID == nil {
		return cc
	}
	cc.Screen = &screenSession{
		threadID:  threadID,
		projectID: cc.View.ProjectID,
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
