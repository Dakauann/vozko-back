package unofficial_whatsapp

import (
	"context"
	"errors"
	"fmt"
	"time"

	uw "vozko/domain/unofficial_whatsapp"
)

var (
	ErrNotOnWhatsApp = errors.New("unofficial whatsapp: this number is not on WhatsApp")
	ErrInvalidPhone  = errors.New("unofficial whatsapp: not a valid phone number")
)

const minPhoneDigits = 8

type StartConversationUseCase struct {
	instances     uw.InstanceRepository
	servers       uw.ServerRepository
	contacts      uw.ContactRepository
	conversations uw.ConversationRepository
	messaging     uw.MessagingAPI
	leads         LeadLinker
}

func NewStartConversationUseCase(
	instances uw.InstanceRepository,
	servers uw.ServerRepository,
	contacts uw.ContactRepository,
	conversations uw.ConversationRepository,
	messaging uw.MessagingAPI,
	leads LeadLinker,
) *StartConversationUseCase {
	switch {
	case instances == nil:
		panic("unofficial whatsapp: start conversation needs an instance repository")
	case servers == nil:
		panic("unofficial whatsapp: start conversation needs a server repository")
	case contacts == nil:
		panic("unofficial whatsapp: start conversation needs a contact repository")
	case conversations == nil:
		panic("unofficial whatsapp: start conversation needs a conversation repository")
	case messaging == nil:
		panic("unofficial whatsapp: start conversation needs a messaging API")
	}
	return &StartConversationUseCase{
		instances:     instances,
		servers:       servers,
		contacts:      contacts,
		conversations: conversations,
		messaging:     messaging,
		leads:         leads,
	}
}

type StartConversationInput struct {
	WorkspaceID string
	InstanceID  string
	PhoneNumber string
	Name        string
	Scope       uw.DepartmentScope
}

type StartedConversation struct {
	ConversationID string
	ContactID      string
	PhoneNumber    string
	DisplayName    string
	AlreadyExisted bool
}

func (uc *StartConversationUseCase) Execute(
	ctx context.Context,
	in StartConversationInput,
) (*StartedConversation, error) {
	phone := uw.NormalizePhone(in.PhoneNumber)
	if len(phone) < minPhoneDigits {
		return nil, ErrInvalidPhone
	}

	instance, err := uc.instances.FindByID(ctx, in.InstanceID)
	if err != nil {
		return nil, err
	}
	if err := EnsureVisible(instance, in.WorkspaceID, in.Scope); err != nil {
		return nil, err
	}
	if ok, err := instance.CanSend(time.Now().UTC()); !ok {
		return nil, err
	}

	server, err := uc.servers.FindByID(ctx, instance.ServerID)
	if err != nil {
		return nil, err
	}
	ref := uw.RefFor(server, instance)

	checks, err := uc.messaging.CheckNumbers(ctx, ref, []string{phone})
	if err != nil {
		return nil, fmt.Errorf("unofficial whatsapp: could not verify the number: %w", err)
	}
	if len(checks) == 0 || !checks[0].IsOnWhatsApp {
		return nil, ErrNotOnWhatsApp
	}
	check := checks[0]

	resolved, err := uc.resolver().Resolve(ctx, instance, ResolveInput{
		JID:         check.JID,
		LID:         check.LID,
		PhoneNumber: phone,
		Name:        firstNonEmpty(check.VerifiedName, in.Name),
	})
	if err != nil {
		return nil, err
	}
	contact, conv, alreadyExisted := resolved.Contact, resolved.Conversation, resolved.AlreadyExisted

	return &StartedConversation{
		ConversationID: conv.ID,
		ContactID:      contact.ID,
		PhoneNumber:    phone,
		DisplayName:    contact.DisplayName(),
		AlreadyExisted: alreadyExisted,
	}, nil
}

func (uc *StartConversationUseCase) resolver() *ConversationResolver {
	return NewConversationResolver(uc.contacts, uc.conversations, uc.leads)
}
