package copilot_usecase

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"time"

	"vozko/domain/ai"
	"vozko/domain/aichat"
	"vozko/domain/copilot"
	"vozko/usecases/agentloop"
)

const (
	EventQueued    = "queued"
	EventUnqueued  = "unqueued"
	EventDelivered = "delivered"
)

var ErrQueueUnavailable = errors.New("copilot: the message queue is not available")

type QueuedView struct {
	ID          string               `json:"id,omitempty"`
	ClientID    string               `json:"clientId"`
	Content     string               `json:"content"`
	Attachments []copilot.Attachment `json:"attachments,omitempty"`
	CreatedAt   time.Time            `json:"createdAt"`
}

func ViewOfQueued(q *aichat.QueuedMessage) QueuedView {
	return QueuedView{ID: q.MessageID, ClientID: q.ClientID, Content: q.Content, Attachments: storedAttachments(q.Attachments), CreatedAt: q.CreatedAt}
}

func viewsOfQueued(list []*aichat.QueuedMessage) []QueuedView {
	out := make([]QueuedView, len(list))
	for i, q := range list {
		out[i] = ViewOfQueued(q)
	}
	return out
}

func (s *Service) SetQueue(queue aichat.QueueRepository) {
	s.queue = queue
}

func (s *Service) Enqueue(thread *aichat.Thread, userID string, msg copilot.UserMessage) (*aichat.QueuedMessage, error) {
	if s.queue == nil {
		return nil, ErrQueueUnavailable
	}
	if err := aichat.ValidClientID(msg.ClientID); err != nil {
		return nil, err
	}
	content, attachments, err := s.prepare(thread.WorkspaceID, msg)
	if err != nil {
		return nil, err
	}
	queued := &aichat.QueuedMessage{ThreadID: thread.ID, ClientID: msg.ClientID, Content: content}
	if len(attachments) > 0 {
		if queued.Attachments, err = json.Marshal(attachments); err != nil {
			return nil, err
		}
	}
	err = s.turns.Hold(thread.ID, userID, func() (bool, error) {
		return s.messages.HasPendingProposal(thread.ID)
	}, func(running *Turn) error {
		if err := s.queue.Enqueue(queued); err != nil {
			return err
		}
		if running != nil && !queued.Delivered() {
			running.emitLocked(EventQueued, ViewOfQueued(queued))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return queued, nil
}

func (s *Service) Unqueue(thread *aichat.Thread, userID, clientID string) error {
	if s.queue == nil {
		return ErrQueueUnavailable
	}
	if err := s.queue.Remove(thread.ID, clientID); err != nil {
		return err
	}
	if turn, err := s.turns.Observe(thread.ID, userID); err == nil {
		turn.Emit(EventUnqueued, map[string]string{"clientId": clientID})
	}
	return nil
}

func (s *Service) Queued(threadID string) ([]QueuedView, error) {
	if s.queue == nil {
		return []QueuedView{}, nil
	}
	pending, err := s.queue.Pending(threadID)
	if err != nil {
		return nil, err
	}
	return viewsOfQueued(pending), nil
}

func (s *Service) Start(turn *Turn, thread *aichat.Thread, cc copilot.Context, run TurnFunc) {
	turn.start(run, s.followUp(turn, thread, cc))
}

func (s *Service) followUp(turn *Turn, thread *aichat.Thread, cc copilot.Context) func() TurnFunc {
	return func() TurnFunc {
		cc.View = turn.screenLocked(cc.View)
		if !s.awaitsAnswer(thread.ID) {
			return s.resumeOpened(turn, thread, cc)
		}
		next := cc
		return func(ctx context.Context, emit agentloop.Emit) error {
			return s.runTurn(ctx, thread, turnStart{fromQueue: true}, next, emit)
		}
	}
}

func (s *Service) awaitsAnswer(threadID string) bool {
	queued, err := s.hasQueued(threadID)
	if err != nil {
		log.Printf("[copilot] thread %s: queue unreadable, queued messages wait for the next send: %v", threadID, err)
		return false
	}
	if !queued {
		return false
	}
	waiting, err := s.messages.HasPendingProposal(threadID)
	if err != nil {
		log.Printf("[copilot] thread %s: proposal state unreadable, queued messages wait for the next send: %v", threadID, err)
		return false
	}
	return !waiting
}

func (s *Service) hasQueued(threadID string) (bool, error) {
	if s.queue == nil {
		return false, nil
	}
	pending, err := s.queue.Pending(threadID)
	return len(pending) > 0, err
}

func (s *Service) deliverQueued(threadID string, emit agentloop.Emit) ([]*aichat.QueuedMessage, error) {
	if s.queue == nil {
		return nil, nil
	}
	delivered, err := s.queue.Deliver(threadID)
	if err != nil || len(delivered) == 0 {
		return nil, err
	}
	emit(EventDelivered, map[string]interface{}{"messages": viewsOfQueued(delivered)})
	return delivered, nil
}

func (s *Service) inbox(threadID, model string, sees bool, rec *turnRecorder, emit agentloop.Emit) func(context.Context, *agentloop.Session) []ai.Message {
	return func(_ context.Context, sess *agentloop.Session) []ai.Message {
		queued, err := s.hasQueued(threadID)
		if err != nil {
			log.Printf("[copilot] thread %s: queue unreadable mid-answer: %v", threadID, err)
			return nil
		}
		if !queued {
			return nil
		}
		s.saveSegment(threadID, model, sess, rec)
		delivered, err := s.deliverQueued(threadID, emit)
		if err != nil {
			log.Printf("[copilot] thread %s: queued messages not delivered mid-answer: %v", threadID, err)
			return nil
		}
		out := make([]ai.Message, len(delivered))
		for i, q := range delivered {
			out[i] = userMessage(q.Message(), agentloop.Interjection, sees)
		}
		return out
	}
}

func (s *Service) saveSegment(threadID, model string, sess *agentloop.Session, rec *turnRecorder) {
	content := lastAssistantContent(sess.History)
	if content == "" && len(rec.tools) == 0 {
		return
	}
	m, err := rec.message(threadID, content, model, sess.Turn)
	if err == nil {
		err = s.messages.Create(m)
	}
	if err != nil {
		log.Printf("[copilot] thread %s: the reply so far was not saved before a delivered message: %v", threadID, err)
		return
	}
	sess.TakeTurn()
	rec.reset()
}
