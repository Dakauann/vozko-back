package unofficial_whatsapp

import (
	"context"
	"log"

	uw "vozko/domain/unofficial_whatsapp"
)

// ConversationResolver turns a phone number into the contact and conversation
// the CRM already knows.
//
// Extracted from StartConversationUseCase rather than reimplemented for
// campaigns, and that is the most load-bearing reuse in the whole feature. A
// campaign that created contacts its own way would produce a DUPLICATE of every
// person it reached the moment they replied: the inbound webhook resolves people
// by JID, would find nothing matching what the campaign wrote, and would open a
// second contact and a second conversation for the same human.
//
// So there is exactly one path from "a number" to "a conversation", and cold
// outbound, campaigns and inbound messages all take it.
type ConversationResolver struct {
	contacts      uw.ContactRepository
	conversations uw.ConversationRepository
	leads         LeadLinker
}

func NewConversationResolver(
	contacts uw.ContactRepository,
	conversations uw.ConversationRepository,
	leads LeadLinker,
) *ConversationResolver {
	return &ConversationResolver{contacts: contacts, conversations: conversations, leads: leads}
}

// ResolveInput is one subject to resolve.
type ResolveInput struct {
	// JID is WhatsApp's authoritative identifier, from a registration check.
	//
	// Required. Constructing "<phone>@s.whatsapp.net" by hand addresses the
	// wrong identity whenever an account has been migrated, so callers verify
	// the number first and pass what WhatsApp answered.
	JID string
	LID string
	// PhoneNumber in normalized digits.
	PhoneNumber string
	// Name is used only when the contact is new; a number already known keeps
	// the name WhatsApp gave it.
	Name string
}

// Resolved is the pair every caller needs.
type Resolved struct {
	Conversation *uw.Conversation
	Contact      *uw.Contact
	// AlreadyExisted reports whether the conversation was already in the inbox,
	// so a caller can say "opened" rather than implying it made a duplicate.
	AlreadyExisted bool
}

// Resolve finds or creates the contact, bridges its CRM lead, and finds or
// creates the conversation.
func (r *ConversationResolver) Resolve(
	ctx context.Context,
	instance *uw.Instance,
	in ResolveInput,
) (*Resolved, error) {
	contact, err := r.contacts.FindOrCreate(ctx, uw.FindOrCreateContactInput{
		WorkspaceID: instance.WorkspaceID,
		InstanceID:  instance.ID,
		JID:         in.JID,
		LID:         in.LID,
		PhoneNumber: in.PhoneNumber,
		Name:        in.Name,
	})
	if err != nil {
		return nil, err
	}

	r.bridgeContactLead(ctx, instance, contact)

	existing, findErr := r.conversations.FindByChatID(ctx, instance.ID, in.JID)
	alreadyExisted := findErr == nil && existing != nil

	conv, err := r.conversations.FindOrCreate(ctx, uw.FindOrCreateConversationInput{
		WorkspaceID: instance.WorkspaceID,
		InstanceID:  instance.ID,
		ContactID:   contact.ID,
		ChatID:      in.JID,
		IsGroup:     false,
	})
	if err != nil {
		return nil, err
	}

	return &Resolved{Conversation: conv, Contact: contact, AlreadyExisted: alreadyExisted}, nil
}

// bridgeContactLead attaches the CRM lead, exactly as the inbound path does.
//
// Best-effort for the same reason it is there: a lead that could not be created
// must not stop an operator from reaching someone, and the next inbound message
// retries the bridge.
func (r *ConversationResolver) bridgeContactLead(
	ctx context.Context,
	instance *uw.Instance,
	contact *uw.Contact,
) {
	if r.leads == nil || contact.LeadID != nil || contact.PhoneNumber == "" {
		return
	}
	leadID, err := r.leads.EnsureLeadForPhone(
		ctx, instance.WorkspaceID, contact.PhoneNumber, contact.DisplayName())
	if err != nil || leadID == "" {
		log.Printf("[unofficial-whatsapp] could not bridge a lead for contact %s: %v", contact.ID, err)
		return
	}
	if err := r.contacts.LinkLead(ctx, contact.ID, leadID); err != nil {
		log.Printf("[unofficial-whatsapp] could not attach lead %s to contact %s: %v",
			leadID, contact.ID, err)
		return
	}
	contact.LeadID = &leadID
}
