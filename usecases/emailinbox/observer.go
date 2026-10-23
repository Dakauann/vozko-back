package emailinbox

import (
	"context"
	"errors"

	"vozko/domain/conversation"
	em "vozko/domain/emailinbox"
	"vozko/domain/emailsending"
	"vozko/domain/shared"
	"vozko/usecases/messagestate"
)

type DeliveryObserverDeps struct {
	Messages      MessageStore
	Contacts      em.ContactRepository
	Conversations em.ConversationRepository
	Announcer     messagestate.Announcer
}

type DeliveryObserver struct {
	messages      MessageStore
	contacts      em.ContactRepository
	conversations em.ConversationRepository
	announcer     messagestate.Announcer
}

var _ emailsending.MessageObserver = (*DeliveryObserver)(nil)

func NewDeliveryObserver(d DeliveryObserverDeps) *DeliveryObserver {
	if d.Messages == nil || d.Contacts == nil || d.Conversations == nil || d.Announcer == nil {
		panic("emailinbox: the delivery observer is missing a dependency")
	}
	return &DeliveryObserver{messages: d.Messages, contacts: d.Contacts, conversations: d.Conversations, announcer: d.Announcer}
}

func (o *DeliveryObserver) OnMessageEvent(ctx context.Context, m emailsending.Message, ev emailsending.DeliveryEvent, effect emailsending.EventEffect) error {
	if m.ProviderMessageID == "" || !effect.Changed {
		return nil
	}
	next, ok := transcriptStatus(ev)
	if !ok {
		return nil
	}
	entryID, found, err := o.transcriptEntry(ctx, m)
	if err != nil || !found {
		return err
	}
	message, err := o.messages.GetByEntryAndExternalMessageID(shared.EntryTypeEmail, entryID, m.ProviderMessageID)
	if errors.Is(err, conversation.ErrMessageNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if !next.Advances(message.DeliveryStatus) {
		return nil
	}
	message.DeliveryStatus = next
	return messagestate.Writer{Messages: o.messages, Announcer: o.announcer}.Save(message)
}

func (o *DeliveryObserver) transcriptEntry(ctx context.Context, m emailsending.Message) (string, bool, error) {
	switch m.RunKind {
	case emailsending.RunReply:
		return m.RunID, m.RunID != "", nil
	case emailsending.RunTriggered:
		return o.recipientConversation(ctx, m)
	}
	return "", false, nil
}

func (o *DeliveryObserver) recipientConversation(ctx context.Context, m emailsending.Message) (string, bool, error) {
	contact, err := o.contacts.FindByAddress(ctx, m.WorkspaceID, m.Address)
	if errors.Is(err, em.ErrContactNotFound) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	conv, err := o.conversations.FindByContact(ctx, m.WorkspaceID, contact.ID)
	if errors.Is(err, em.ErrConversationNotFound) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return conv.ID, true, nil
}

func transcriptStatus(ev emailsending.DeliveryEvent) (conversation.DeliveryStatus, bool) {
	switch ev.Kind {
	case emailsending.EventDelivery:
		return conversation.DeliveryStatusDelivered, true
	case emailsending.EventBounce, emailsending.EventReject, emailsending.EventRenderingFailure:
		return conversation.DeliveryStatusFailed, true
	}
	return "", false
}
