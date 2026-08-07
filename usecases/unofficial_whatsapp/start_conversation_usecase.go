package unofficial_whatsapp

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	uw "vozko/domain/unofficial_whatsapp"
)

// Errors an operator can act on, distinct from an infrastructure failure so the
// UI can say what to do rather than "something went wrong".
var (
	// ErrNotOnWhatsApp means the number exists but has no WhatsApp account.
	ErrNotOnWhatsApp = errors.New("unofficial whatsapp: this number is not on WhatsApp")
	// ErrInvalidPhone means the number is not usable as an address at all.
	ErrInvalidPhone = errors.New("unofficial whatsapp: not a valid phone number")
)

// minPhoneDigits is the shortest string that could be a real international
// number. Below this the operator has mistyped rather than reached anyone, and
// sending to it is a wasted provider call against the instance's budget.
const minPhoneDigits = 8

// StartConversationUseCase opens a conversation with a number nobody has
// messaged us from.
//
// This is the channel's answer to cold outbound, and it exists here rather than
// as a one-off in the CRM because it must reuse the SAME contact, lead and
// conversation resolution the inbound path uses. A second "create a contact"
// code path would produce a duplicate contact the moment the person replies:
// the webhook would resolve them by JID, find nothing matching what the operator
// typed, and open a second conversation for the same human.
//
// It deliberately stops at "the conversation exists". Sending is left to the
// ordinary composer, which already carries pacing, window checks, restriction
// handling and persistence — so this use case cannot drift from how every other
// message on the channel is sent.
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
	return &StartConversationUseCase{
		instances:     instances,
		servers:       servers,
		contacts:      contacts,
		conversations: conversations,
		messaging:     messaging,
		leads:         leads,
	}
}

// StartConversationInput is one operator's request to reach a number.
type StartConversationInput struct {
	WorkspaceID string
	InstanceID  string
	// PhoneNumber as the operator typed it. Normalised here rather than in the
	// handler, so every caller gets the same treatment.
	PhoneNumber string
	// Name is optional and only used when the contact is new; a number already
	// known keeps the name WhatsApp gave it.
	Name string
}

// StartedConversation is where the operator should be taken.
type StartedConversation struct {
	ConversationID string
	ContactID      string
	// PhoneNumber is the normalised form actually addressed, which may differ
	// from what was typed.
	PhoneNumber string
	// DisplayName is the profile name WhatsApp reports, when it has one. Worth
	// returning: it is the operator's confirmation they reached the right person.
	DisplayName string
	// AlreadyExisted is true when this conversation was already in the inbox.
	// The operator is taken to the same place either way, but the UI can say
	// "opened" rather than "created" instead of implying a duplicate was made.
	AlreadyExisted bool
}

// Execute resolves the number and opens the conversation.
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
	// Ownership is checked here as well as by the route's permission gate: the
	// gate proves the caller may send from SOME instance in their workspace,
	// not from this one.
	if instance.WorkspaceID != in.WorkspaceID {
		return nil, uw.ErrInstanceNotFound
	}
	if ok, err := instance.CanSend(time.Now().UTC()); !ok {
		return nil, err
	}

	server, err := uc.servers.FindByID(ctx, instance.ServerID)
	if err != nil {
		return nil, err
	}
	ref := uw.RefFor(server, instance)

	// The number is verified against WhatsApp BEFORE anything is written.
	//
	// This is the single most valuable step in the flow. Messaging numbers that
	// are not on WhatsApp is one of the strongest ban signals there is, and an
	// operator pasting a number from a spreadsheet has no way to know. It also
	// yields the authoritative JID: constructing "<phone>@s.whatsapp.net" by
	// hand addresses the wrong identity whenever the account has been migrated.
	checks, err := uc.messaging.CheckNumbers(ctx, ref, []string{phone})
	if err != nil {
		return nil, fmt.Errorf("unofficial whatsapp: could not verify the number: %w", err)
	}
	if len(checks) == 0 || !checks[0].IsOnWhatsApp {
		return nil, ErrNotOnWhatsApp
	}
	check := checks[0]

	// From here the flow is the INBOUND flow, deliberately. Same repositories,
	// same lead bridge, same conversation resolution — so a contact opened by an
	// operator and one opened by an incoming message are the same record.
	contact, err := uc.contacts.FindOrCreate(ctx, uw.FindOrCreateContactInput{
		WorkspaceID: instance.WorkspaceID,
		InstanceID:  instance.ID,
		JID:         check.JID,
		LID:         check.LID,
		PhoneNumber: phone,
		Name:        firstNonEmpty(check.VerifiedName, in.Name),
	})
	if err != nil {
		return nil, err
	}

	uc.bridgeContactLead(ctx, instance, contact)

	existing, findErr := uc.conversations.FindByChatID(ctx, instance.ID, check.JID)
	alreadyExisted := findErr == nil && existing != nil

	conv, err := uc.conversations.FindOrCreate(ctx, uw.FindOrCreateConversationInput{
		WorkspaceID: instance.WorkspaceID,
		InstanceID:  instance.ID,
		ContactID:   contact.ID,
		ChatID:      check.JID,
		IsGroup:     false,
	})
	if err != nil {
		return nil, err
	}

	return &StartedConversation{
		ConversationID: conv.ID,
		ContactID:      contact.ID,
		PhoneNumber:    phone,
		DisplayName:    contact.DisplayName(),
		AlreadyExisted: alreadyExisted,
	}, nil
}

// bridgeContactLead attaches the CRM lead, exactly as the inbound path does.
//
// Best-effort for the same reason it is there: a lead that could not be created
// must not stop an operator from reaching someone. The next inbound message
// retries the bridge.
func (uc *StartConversationUseCase) bridgeContactLead(
	ctx context.Context,
	instance *uw.Instance,
	contact *uw.Contact,
) {
	if uc.leads == nil || contact.LeadID != nil || contact.PhoneNumber == "" {
		return
	}
	leadID, err := uc.leads.EnsureLeadForPhone(
		ctx, instance.WorkspaceID, contact.PhoneNumber, contact.DisplayName())
	if err != nil || leadID == "" {
		log.Printf("[unofficial-whatsapp] could not bridge a lead for contact %s: %v", contact.ID, err)
		return
	}
	if err := uc.contacts.LinkLead(ctx, contact.ID, leadID); err != nil {
		log.Printf("[unofficial-whatsapp] could not attach lead %s to contact %s: %v",
			leadID, contact.ID, err)
		return
	}
	contact.LeadID = &leadID
}
