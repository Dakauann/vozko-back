package emailinbox

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/google/uuid"

	"vozko/domain/balance"
	"vozko/domain/channel"
	"vozko/domain/conversation"
	"vozko/domain/emailaddress"
	"vozko/domain/emailhygiene"
	em "vozko/domain/emailinbox"
	"vozko/domain/emailsending"
	"vozko/domain/media"
	"vozko/domain/shared"
)

const SendCodeBalanceInsufficient = "email_balance_insufficient"

func replyKey(conversationID, sendID string) string {
	return "email_reply:" + conversationID + ":" + sendID
}

type AdapterDeps struct {
	Conversations em.ConversationRepository
	Contacts      em.ContactRepository
	Accounts      SenderAccounts
	Suppressions  Suppressions
	Billing       ReplyBilling
	Gateway       Gateway
	Files         media.RemoteFiles
	Media         conversation.MediaStore
}

type ChannelAdapter struct {
	d     AdapterDeps
	caps  channel.Capabilities
	now   Clock
	newID func() string
}

var (
	_ conversation.ChannelAdapter      = (*ChannelAdapter)(nil)
	_ conversation.CapabilitiesAdapter = (*ChannelAdapter)(nil)
	_ conversation.WindowStatesAdapter = (*ChannelAdapter)(nil)
)

func NewChannelAdapter(d AdapterDeps) *ChannelAdapter {
	if d.Conversations == nil || d.Contacts == nil || d.Accounts == nil || d.Suppressions == nil || d.Billing == nil || d.Gateway == nil || d.Files == nil || d.Media == nil {
		panic("emailinbox: the channel adapter is missing a dependency")
	}
	return &ChannelAdapter{d: d, caps: em.Descriptor().Capabilities, now: utcNow, newID: uuid.NewString}
}

func (a *ChannelAdapter) EntryType() shared.EntryType { return shared.EntryTypeEmail }

func (a *ChannelAdapter) Capabilities() channel.Capabilities { return a.caps }

func (a *ChannelAdapter) ResolveEntry(ctx context.Context, entryID string) (*conversation.EntryContext, error) {
	conv, err := a.d.Conversations.FindByID(ctx, entryID)
	if err != nil {
		return nil, err
	}
	contact, err := a.d.Contacts.FindByID(ctx, conv.ContactID)
	if err != nil {
		return nil, err
	}
	return &conversation.EntryContext{
		EntryID:       conv.ID,
		EntryType:     shared.EntryTypeEmail,
		WorkspaceID:   conv.WorkspaceID,
		AccountID:     conv.SenderID,
		ContactID:     contact.ID,
		ContactRef:    string(contact.Address),
		ContactHandle: string(contact.Address),
		LastInboundAt: conv.LastCustomerMessageAt,
	}, nil
}

func (a *ChannelAdapter) WindowState(ctx context.Context, ec *conversation.EntryContext) (conversation.WindowState, error) {
	if ec == nil {
		return conversation.ClosedWindow(conversation.WindowReasonChannelUnavailable), conversation.ErrNoAdapterForEntryType
	}
	states, err := a.windows(ctx, map[string][]windowSubject{ec.WorkspaceID: {{entryID: ec.EntryID, address: emailaddress.Address(ec.ContactRef)}}})
	if err != nil {
		return conversation.ClosedWindow(conversation.WindowReasonChannelUnavailable), err
	}
	return states[ec.EntryID], nil
}

type windowSubject struct {
	entryID string
	address emailaddress.Address
}

func (a *ChannelAdapter) WindowStates(ctx context.Context, entryIDs []string) (map[string]conversation.WindowState, error) {
	convs, err := a.d.Conversations.FindByIDs(ctx, shared.UniqueNonEmpty(entryIDs))
	if err != nil {
		return nil, err
	}
	contactIDs := make([]string, 0, len(convs))
	for _, conv := range convs {
		contactIDs = append(contactIDs, conv.ContactID)
	}
	contacts, err := a.d.Contacts.FindByIDs(ctx, shared.UniqueNonEmpty(contactIDs))
	if err != nil {
		return nil, err
	}
	addressByContact := make(map[string]emailaddress.Address, len(contacts))
	for _, c := range contacts {
		addressByContact[c.ID] = c.Address
	}
	byWorkspace := map[string][]windowSubject{}
	unavailable := map[string]conversation.WindowState{}
	for _, conv := range convs {
		address, ok := addressByContact[conv.ContactID]
		if !ok {
			unavailable[conv.ID] = conversation.ClosedWindow(conversation.WindowReasonChannelUnavailable)
			continue
		}
		byWorkspace[conv.WorkspaceID] = append(byWorkspace[conv.WorkspaceID], windowSubject{entryID: conv.ID, address: address})
	}
	states, err := a.windows(ctx, byWorkspace)
	if err != nil {
		return nil, err
	}
	for id, state := range unavailable {
		states[id] = state
	}
	return states, nil
}

func (a *ChannelAdapter) windows(ctx context.Context, byWorkspace map[string][]windowSubject) (map[string]conversation.WindowState, error) {
	out := map[string]conversation.WindowState{}
	for workspaceID, subjects := range byWorkspace {
		account, err := a.d.Accounts.GetByWorkspace(ctx, workspaceID)
		switch {
		case errors.Is(err, emailsending.ErrAccountNotFound):
			account = nil
		case err != nil:
			return nil, err
		}
		addresses := make([]emailaddress.Address, 0, len(subjects))
		for _, s := range subjects {
			addresses = append(addresses, s.address)
		}
		suppressed, err := a.d.Suppressions.Suppressed(ctx, workspaceID, addresses)
		if err != nil {
			return nil, err
		}
		for _, s := range subjects {
			_, blocked := suppressed[s.address]
			out[s.entryID] = em.WindowOf(account, blocked)
		}
	}
	return out, nil
}

func (a *ChannelAdapter) SendText(ctx context.Context, ec *conversation.EntryContext, req conversation.SendTextRequest) (*conversation.SendOutcome, error) {
	if ec == nil {
		return nil, conversation.ErrNoAdapterForEntryType
	}
	body := strings.TrimSpace(req.Body)
	if body == "" {
		return nil, em.ErrEmptyReply
	}
	if a.caps.TextTooLong(body) {
		return nil, em.ErrReplyTooLong
	}
	return a.deliver(ctx, ec, outgoingReply{body: body})
}

func (a *ChannelAdapter) SendMedia(ctx context.Context, ec *conversation.EntryContext, req conversation.SendMediaRequest) (*conversation.SendOutcome, error) {
	if ec == nil {
		return nil, conversation.ErrNoAdapterForEntryType
	}
	caption := strings.TrimSpace(req.Caption)
	if a.caps.TextTooLong(caption) {
		return nil, em.ErrReplyTooLong
	}
	attachment, err := a.replyAttachment(ctx, req)
	if err != nil {
		return nil, err
	}
	body := caption
	if body == "" {
		body = attachment.FileName
	}
	return a.deliver(ctx, ec, outgoingReply{
		body:        body,
		attachments: []emailsending.Attachment{attachment},
		metadata:    map[string]any{em.MetadataFileName: attachment.FileName},
	})
}

type outgoingReply struct {
	body        string
	attachments []emailsending.Attachment
	metadata    map[string]any
}

func (a *ChannelAdapter) deliver(ctx context.Context, ec *conversation.EntryContext, reply outgoingReply) (*conversation.SendOutcome, error) {
	conv, err := a.d.Conversations.FindByID(ctx, ec.EntryID)
	if err != nil {
		return nil, err
	}
	contact, err := a.d.Contacts.FindByID(ctx, conv.ContactID)
	if err != nil {
		return nil, err
	}
	key := replyKey(conv.ID, a.newID())
	charge, err := a.d.Billing.ChargeMessageWithAttachments(ctx, conv.WorkspaceID, key, emailsending.EncodedAttachmentsBytes(reply.attachments))
	if err != nil {
		if errors.Is(err, balance.ErrInsufficientBalance) {
			return nil, &conversation.SendRefusal{Code: SendCodeBalanceInsufficient, Err: err}
		}
		return nil, err
	}
	subject := em.ReplySubject(conv.Subject)
	sent, err := a.d.Gateway.Send(ctx, emailsending.SendRequest{
		WorkspaceID: conv.WorkspaceID,
		SenderID:    conv.SenderID,
		To:          contact.Address,
		Kind:        emailsending.KindTransactional,
		RunKind:     emailsending.RunReply,
		RunID:       conv.ID,
		InReplyTo:   conv.LastInboundMessageID,
		References:  conv.References,
	}, func(emailsending.Envelope) (emailsending.Content, error) {
		return emailsending.Content{Subject: subject, Text: reply.body, HTML: em.ReplyHTML(reply.body), Attachments: reply.attachments}, nil
	})
	if err != nil {
		if refundErr := a.d.Billing.RefundMessage(ctx, conv.WorkspaceID, key, charge); refundErr != nil {
			log.Printf("%s CRITICAL reply on %s failed and its charge was not refunded: %v", logPrefix, conv.ID, refundErr)
		}
		if code, refused := emailhygiene.RecipientRefusalCode(err); refused {
			return nil, &conversation.SendRefusal{Code: code, Err: err}
		}
		return nil, fmt.Errorf("emailinbox: reply on %s not sent: %w", conv.ID, err)
	}
	if err := a.d.Conversations.RecordOutbound(ctx, conv.ID, a.now()); err != nil {
		log.Printf("%s record outbound conversation=%s: %v", logPrefix, conv.ID, err)
	}
	metadata := map[string]any{em.MetadataSubject: subject}
	if id := storeHTML(a.d.Media, conv.ID, em.ReplyHTML(reply.body)); id != "" {
		metadata[em.MetadataHTMLMediaID] = id
	}
	for k, v := range reply.metadata {
		metadata[k] = v
	}
	return &conversation.SendOutcome{
		ProviderMessageID: em.OutboundExternalID(sent.ProviderMessageID, sent.ID),
		Metadata:          metadata,
	}, nil
}
