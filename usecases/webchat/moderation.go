package webchat

import (
	"context"
	"log"
	"time"

	"vozko/domain/shared"
	wcdomain "vozko/domain/webchat"
)

type Moderation struct {
	conversations wcdomain.ConversationRepository
	visitors      wcdomain.VisitorRepository
	access        shared.EntryAccessChecker
	events        wcdomain.EventPublisher
	operators     OperatorNotifier
	now           func() time.Time
}

func NewModeration(
	conversations wcdomain.ConversationRepository,
	visitors wcdomain.VisitorRepository,
	access shared.EntryAccessChecker,
	events wcdomain.EventPublisher,
	operators OperatorNotifier,
) *Moderation {
	return &Moderation{
		conversations: conversations,
		visitors:      visitors,
		access:        access,
		events:        events,
		operators:     operators,
		now:           func() time.Time { return time.Now().UTC() },
	}
}

func (m *Moderation) SetBlocked(ctx context.Context, person shared.Person, workspaceID, entryID string, blocked bool) error {
	if !person.MayActOn(m.access, workspaceID, entryID, string(shared.EntryTypeWebchat)) {
		return wcdomain.ErrConversationNotFound
	}
	conv, err := m.conversations.FindByID(ctx, entryID)
	if err != nil {
		return err
	}
	if conv.WorkspaceID != workspaceID {
		return wcdomain.ErrConversationNotFound
	}
	if err := m.visitors.SetBlocked(ctx, conv.VisitorID, blocked, m.now()); err != nil {
		return err
	}
	if blocked {
		if err := m.events.Publish(ctx, wcdomain.VisitorEvent{VisitorID: conv.VisitorID, Kind: wcdomain.EventStatus, State: wcdomain.StateBlocked}); err != nil {
			log.Printf("[webchat] tell visitor=%s they are blocked: %v", conv.VisitorID, err)
		}
	}
	m.operators.BroadcastEntryUpdate(conv.ID, string(shared.EntryTypeWebchat), nil)
	return nil
}
