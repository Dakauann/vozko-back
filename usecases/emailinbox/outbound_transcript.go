package emailinbox

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"strings"
	"time"

	"vozko/domain/conversation"
	em "vozko/domain/emailinbox"
	"vozko/domain/emailsending"
	"vozko/domain/shared"
)

type OutboundSenders interface {
	Get(ctx context.Context, workspaceID, id string) (*emailsending.Sender, error)
	GetDefault(ctx context.Context, workspaceID string) (*emailsending.Sender, error)
}

type OutboundTranscriptDeps struct {
	Contacts      em.ContactRepository
	Conversations em.ConversationRepository
	Inboxes       em.InboxRepository
	Senders       OutboundSenders
	Domains       SendingDomains
	History       conversation.MessageHistoryManager
	Messages      MessageStore
	Media         conversation.MediaStore
}

type OutboundTranscript struct {
	d   OutboundTranscriptDeps
	now Clock
}

var _ em.SentEmailTranscript = (*OutboundTranscript)(nil)

func NewOutboundTranscript(d OutboundTranscriptDeps) *OutboundTranscript {
	if d.Contacts == nil || d.Conversations == nil || d.Inboxes == nil || d.Senders == nil || d.Domains == nil || d.History == nil || d.Messages == nil || d.Media == nil {
		panic("emailinbox: the outbound transcript is missing a dependency")
	}
	return &OutboundTranscript{d: d, now: utcNow}
}

func (t *OutboundTranscript) Transcribe(ctx context.Context, e em.OutboundEmail) (string, error) {
	if err := e.Validate(); err != nil {
		return "", err
	}
	sender, err := t.sender(ctx, e)
	if err != nil {
		return "", err
	}
	contact, known, err := t.contact(ctx, e)
	if err != nil || !known {
		return "", err
	}
	inbox, err := ensureSenderInbox(ctx, t.d.Domains, t.d.Inboxes, *sender)
	if err != nil {
		return "", err
	}
	conv, _, err := t.d.Conversations.FindOrCreate(ctx, em.FindOrCreateConversationInput{
		WorkspaceID: e.WorkspaceID, ContactID: contact.ID, SenderID: sender.ID,
	})
	if err != nil {
		return "", err
	}
	if err := t.record(ctx, *conv, string(inbox.Address), e); err != nil {
		return "", err
	}
	return conv.ID, nil
}

func (t *OutboundTranscript) TranscribeInto(ctx context.Context, conv em.Conversation, from string, e em.OutboundEmail) error {
	if err := e.Validate(); err != nil {
		return err
	}
	return t.record(ctx, conv, from, e)
}

func (t *OutboundTranscript) Holds(conversationID, externalID string) (bool, error) {
	_, err := t.d.Messages.GetByEntryAndExternalMessageID(shared.EntryTypeEmail, conversationID, externalID)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, conversation.ErrMessageNotFound):
		return false, nil
	}
	return false, err
}

func (t *OutboundTranscript) record(ctx context.Context, conv em.Conversation, from string, e em.OutboundEmail) error {
	held, err := t.Holds(conv.ID, e.ExternalID)
	if err != nil || held {
		return err
	}
	at := e.SentAt
	if at.IsZero() {
		at = t.now()
	}
	if err := t.d.Conversations.ApplyThread(ctx, conv.ID, conv.ThreadAfterOutbound(e.Subject)); err != nil {
		return err
	}
	if err := t.d.Conversations.RecordOutbound(ctx, conv.ID, at); err != nil {
		return err
	}
	return t.d.History.Record(ctx, outboundRecord(e, conv.ID, from, at, storeHTML(t.d.Media, conv.ID, e.HTML)))
}

func (t *OutboundTranscript) sender(ctx context.Context, e em.OutboundEmail) (*emailsending.Sender, error) {
	if id := strings.TrimSpace(e.SenderID); id != "" {
		return t.d.Senders.Get(ctx, e.WorkspaceID, id)
	}
	return t.d.Senders.GetDefault(ctx, e.WorkspaceID)
}

func (t *OutboundTranscript) contact(ctx context.Context, e em.OutboundEmail) (*em.Contact, bool, error) {
	leadID := strings.TrimSpace(e.LeadID)
	if leadID == "" {
		found, err := t.d.Contacts.FindByAddress(ctx, e.WorkspaceID, e.To)
		switch {
		case err == nil:
			return found, true, nil
		case !errors.Is(err, em.ErrContactNotFound):
			return nil, false, err
		case !e.OpensContact:
			return nil, false, nil
		}
	}
	input := em.FindOrCreateContactInput{WorkspaceID: e.WorkspaceID, Address: e.To, DisplayName: e.LeadName}
	if leadID != "" {
		input.LeadID = &leadID
	}
	contact, err := t.d.Contacts.FindOrCreate(ctx, input)
	if err != nil {
		return nil, false, err
	}
	if leadID != "" && contact.LeadID == nil {
		if err := t.d.Contacts.LinkLead(ctx, contact.ID, leadID); err != nil {
			log.Printf("%s lead %s not linked to contact %s: %v", logPrefix, leadID, contact.ID, err)
		} else {
			contact.LeadID = &leadID
		}
	}
	return contact, true, nil
}

func outboundRecord(e em.OutboundEmail, conversationID, from string, at time.Time, htmlMediaID string) conversation.MessageHistoryRecord {
	meta := map[string]any{}
	if htmlMediaID != "" {
		meta[em.MetadataHTMLMediaID] = htmlMediaID
	}
	if e.CampaignMerge != nil && strings.TrimSpace(e.SentMessageID) != "" {
		meta[em.MetadataSentMessageID] = strings.TrimSpace(e.SentMessageID)
		meta[em.MetadataCampaignMerge] = e.CampaignMerge
	}
	if subject := strings.TrimSpace(e.Subject); subject != "" {
		meta[em.MetadataSubject] = subject
	}
	if id := strings.TrimSpace(e.CampaignID); id != "" {
		meta[em.MetadataCampaignID] = id
		meta[em.MetadataCampaignName] = strings.TrimSpace(e.CampaignName)
	}
	text, cut := em.TruncateText(e.Text, em.MaxTranscriptTextBytes)
	if cut {
		meta[em.MetadataTruncated] = true
	}
	raw, _ := json.Marshal(meta)
	return conversation.MessageHistoryRecord{
		SentBy:            e.SentBy,
		EntryID:           conversationID,
		EntryType:         shared.EntryTypeEmail,
		Channel:           conversation.MessageChannelEmail,
		MessageType:       em.OutboundMessageType(e.SentBy),
		ProviderMessageID: e.ExternalID,
		From:              from,
		To:                string(e.To),
		Text:              text,
		Timestamp:         at,
		Metadata:          raw,
		DeliveryStatus:    e.DeliveryStatus,
	}
}
