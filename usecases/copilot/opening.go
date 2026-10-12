package copilot_usecase

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"

	"vozko/domain/ai"
	"vozko/domain/aichat"
	"vozko/domain/copilot"
	"vozko/usecases/agentloop"
)

type OpenOutcome string

const (
	OpenIgnored   OpenOutcome = "ignored"
	OpenDelivered OpenOutcome = "delivered"
	OpenResumed   OpenOutcome = "resumed"
)

var ErrProjectAdoptionUnavailable = errors.New("copilot: the thread cannot adopt a studio project")

func (s *Service) SetProjectAdopter(adopter aichat.ProjectAdopter) {
	s.adopter = adopter
}

func (s *Service) adopt(thread *aichat.Thread, projectID string) error {
	if thread.ProjectID == projectID {
		return nil
	}
	if s.adopter == nil {
		return ErrProjectAdoptionUnavailable
	}
	if err := s.adopter.AdoptProject(thread.ID, projectID); err != nil {
		return err
	}
	thread.ProjectID = projectID
	return nil
}

func (s *Service) ProjectOpened(parent context.Context, thread *aichat.Thread, cc copilot.Context) (OpenOutcome, error) {
	view := cc.View
	if !view.OnStudio() || thread.ProjectID == "" || thread.ProjectID != view.ProjectID {
		return OpenIgnored, nil
	}
	for range 2 {
		delivered, err := s.turns.Offer(thread.ID, cc.UserID, view)
		if err != nil {
			return OpenIgnored, err
		}
		if delivered {
			return OpenDelivered, nil
		}
		note, err := s.awaitedNote(thread.ID, view)
		if err != nil || note == "" {
			return OpenIgnored, err
		}
		turn, err := s.turns.Begin(parent, thread.ID, cc.UserID)
		if errors.Is(err, copilot.ErrTurnRunning) {
			continue
		}
		if err != nil {
			return OpenIgnored, err
		}
		s.Start(turn, thread, cc, s.resume(thread, cc, note))
		return OpenResumed, nil
	}
	return OpenIgnored, nil
}

func (s *Service) resume(thread *aichat.Thread, cc copilot.Context, note string) TurnFunc {
	return func(ctx context.Context, emit agentloop.Emit) error {
		return s.runTurn(ctx, thread, turnStart{content: note, notice: true}, cc, emit)
	}
}

func (s *Service) resumeOpened(turn *Turn, thread *aichat.Thread, cc copilot.Context) TurnFunc {
	view, ok := turn.takeOpenedLocked()
	if !ok {
		return nil
	}
	note, err := s.awaitedNote(thread.ID, view)
	if err != nil {
		log.Printf("[copilot] thread %s: history unreadable, the opened editor waits for the next message: %v", thread.ID, err)
		return nil
	}
	if note == "" {
		return nil
	}
	cc.View = view
	return s.resume(thread, cc, note)
}

func (s *Service) awaitedNote(threadID string, view copilot.View) (string, error) {
	_, total, err := s.messages.ListByThread(aichat.ListMessagesInput{ThreadID: threadID, Limit: 1})
	if err != nil || total == 0 {
		return "", err
	}
	last, _, err := s.messages.ListByThread(aichat.ListMessagesInput{ThreadID: threadID, Limit: 1, Offset: int(total) - 1})
	if err != nil || len(last) == 0 {
		return "", err
	}
	m := last[len(last)-1]
	if m.Role != aichat.RoleAssistant || m.ProposalStatus == aichat.ProposalPending {
		return "", nil
	}
	var steps []toolStep
	if len(m.ToolCalls) == 0 || json.Unmarshal(m.ToolCalls, &steps) != nil {
		return "", nil
	}
	note := ""
	for _, step := range steps {
		switch {
		case step.Awaits == nil || *step.Awaits != view:
		case step.Ok:
			return projectOpenedNote(view), nil
		default:
			note = editorBackNote(view)
		}
	}
	return note, nil
}

func (s *Service) withOpenings(threadID string, rec *turnRecorder, driver *Driver, inbox func(context.Context, *agentloop.Session) []ai.Message) func(context.Context, *agentloop.Session) []ai.Message {
	return func(ctx context.Context, sess *agentloop.Session) []ai.Message {
		var notice []ai.Message
		if view, ok := s.turns.TakeOpened(threadID); ok {
			silent := rec.awaits(view.ProjectID)
			rec.opened(view.ProjectID)
			switch {
			case driver.show(view):
				notice = append(notice, ai.Message{Role: ai.RoleUser, Content: agentloop.AppNotice(projectOpenedNote(view))})
			case silent:
				notice = append(notice, ai.Message{Role: ai.RoleUser, Content: agentloop.AppNotice(editorBackNote(view))})
			}
		}
		return append(notice, inbox(ctx, sess)...)
	}
}

func projectOpenedNote(view copilot.View) string {
	kind := "vídeo"
	if view.ProjectKind == copilot.StudioImage {
		kind = "imagem"
	}
	return fmt.Sprintf("o usuário abriu no Estúdio o projeto de %s que você criou (project_id %s). O editor está aberto nesta resposta e as ferramentas do Estúdio já estão disponíveis: "+
		"continue agora o trabalho que você combinou, começando por studio_read. Não peça que ele confirme que abriu.", kind, view.ProjectID)
}

func editorBackNote(view copilot.View) string {
	return fmt.Sprintf("a aba do projeto (project_id %s) voltou a ficar visível e o editor responde de novo nesta resposta. "+
		"Pedidos que expiraram enquanto ele estava calado não foram aplicados; um pedido que ficou sem confirmação pode ter entrado. "+
		"Confira o estado com studio_read e continue o trabalho que você combinou, sem repetir o que já entrou e sem pedir confirmação ao usuário.", view.ProjectID)
}
