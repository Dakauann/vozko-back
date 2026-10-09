package conversation_usecase

import (
	"context"
	"errors"

	"vozko/domain/callrouting"
	"vozko/domain/shared"
	businessphone "vozko/domain/whatsapp/business_phone"
	wce "vozko/domain/whatsapp_campaign_entry"
)

var ErrCallConversationNotFound = errors.New("the call has no conversation in this workspace")

type CallHandoffPhones interface {
	FindByID(id string) (*businessphone.WhatsAppBusinessPhoneNumber, error)
}

type CallHandoffEntries interface {
	FindByNumberBusinessPhoneAndWorkspace(number, businessPhoneID, workspaceID string) (*wce.WhatsAppCampaignEntry, error)
}

type CallHandoffAssigner interface {
	Check(by shared.Person, workspaceID, entryID, entryType, toUserID string) error
	Assign(by shared.Person, workspaceID, entryID, entryType, toUserID string) error
}

type CallConversationHandoffDeps struct {
	Phones  CallHandoffPhones
	Entries CallHandoffEntries
	Assign  CallHandoffAssigner
}

type callConversationHandoff struct{ deps CallConversationHandoffDeps }

func NewCallConversationHandoff(deps CallConversationHandoffDeps) callrouting.ConversationHandoff {
	return &callConversationHandoff{deps: deps}
}

func (h *callConversationHandoff) MayHandOver(_ context.Context, handover callrouting.Handover) error {
	entryID, err := h.conversationOf(handover)
	if err != nil {
		return err
	}
	return h.deps.Assign.Check(shared.Person{UserID: handover.FromUserID}, handover.WorkspaceID, entryID, string(shared.EntryTypeWhatsApp), handover.ToUserID)
}

func (h *callConversationHandoff) HandOver(_ context.Context, handover callrouting.Handover) error {
	entryID, err := h.conversationOf(handover)
	if err != nil {
		return err
	}
	return h.deps.Assign.Assign(shared.Person{UserID: handover.FromUserID}, handover.WorkspaceID, entryID, string(shared.EntryTypeWhatsApp), handover.ToUserID)
}

func (h *callConversationHandoff) conversationOf(handover callrouting.Handover) (string, error) {
	if !handover.Contact.Known() {
		return "", ErrCallConversationNotFound
	}
	phone, err := h.deps.Phones.FindByID(handover.Contact.BusinessPhoneID)
	if err != nil || phone == nil || !phone.BelongsToWorkspace(handover.WorkspaceID) {
		return "", ErrCallConversationNotFound
	}
	entry, err := h.deps.Entries.FindByNumberBusinessPhoneAndWorkspace(handover.Contact.ContactNumber, phone.ID, handover.WorkspaceID)
	if err != nil || entry == nil {
		return "", ErrCallConversationNotFound
	}
	return entry.ID, nil
}
