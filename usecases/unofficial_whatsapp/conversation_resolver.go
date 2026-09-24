package unofficial_whatsapp

import (
	"context"
	"log"

	uw "vozko/domain/unofficial_whatsapp"
)

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

type ResolveInput struct {
	JID         string
	LID         string
	PhoneNumber string
	Name        string
	// CampaignID opens that campaign's own conversation with the chat, as an
	// official campaign opens its own entry. Empty means the chat's current one.
	CampaignID string
}

type Resolved struct {
	Conversation   *uw.Conversation
	Contact        *uw.Contact
	AlreadyExisted bool
}

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
	alreadyExisted := findErr == nil && existing != nil && existing.CampaignID == in.CampaignID

	conv, err := r.conversations.FindOrCreate(ctx, uw.FindOrCreateConversationInput{
		WorkspaceID: instance.WorkspaceID,
		InstanceID:  instance.ID,
		ContactID:   contact.ID,
		ChatID:      in.JID,
		IsGroup:     false,
		CampaignID:  in.CampaignID,
	})
	if err != nil {
		return nil, err
	}

	return &Resolved{Conversation: conv, Contact: contact, AlreadyExisted: alreadyExisted}, nil
}

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
