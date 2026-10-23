package emailinbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"mime"
	"path"
	"strings"
	"time"

	"github.com/google/uuid"

	"vozko/domain/conversation"
	"vozko/domain/emailaddress"
	"vozko/domain/emailconsent"
	"vozko/domain/emailhygiene"
	em "vozko/domain/emailinbox"
	"vozko/domain/emailsending"
	"vozko/domain/emailtoken"
	"vozko/domain/lead"
	"vozko/domain/shared"
	conversation_usecase "vozko/usecases/conversation"
)

const (
	dispatchClaimChannel = "email-inbound-dispatch"
	logPrefix            = "[emailinbox]"
)

func dispatchClaimKey(conversationID, externalID string) string {
	return "email:dispatch:" + conversationID + ":" + externalID
}

type InboundDeps struct {
	Raw           RawMessages
	Parser        em.MessageParser
	Codec         AddressCodec
	Sent          SentMessages
	ReplyDomains  ReplyDomains
	Senders       Senders
	Domains       SendingDomains
	Unsubscriber  MailtoUnsubscriber
	Inboxes       em.InboxRepository
	Contacts      em.ContactRepository
	Conversations em.ConversationRepository
	Leads         LeadFinder
	History       conversation.MessageHistoryManager
	Messages      MessageStore
	Media         conversation.MediaStore
	Assignments   AssignmentService
	Automation    InboundDispatcher
	Claims        InboundClaims
	Forwarder     *Forwarder
	SenderIP      emailsending.ConnectingIPReader
	Guard         InboundGuard
	Receipts      ReceiveBilling
	Originals     SentOriginals
	Transcript    OriginalTranscript
	Engagement    ReplyEngagement
	EmailsSeen    emailhygiene.AddressSeen
}

type ReplyEngagement interface {
	Replied(ctx context.Context, workspaceID string, address emailaddress.Address, at time.Time) error
}

type Inbound struct {
	d   InboundDeps
	now Clock
}

var _ emailsending.InboundHandler = (*Inbound)(nil)

func NewInbound(d InboundDeps) *Inbound {
	if d.Raw == nil || d.Parser == nil || d.Codec == nil || d.Sent == nil || d.ReplyDomains == nil || d.Senders == nil ||
		d.Domains == nil || d.Unsubscriber == nil || d.Inboxes == nil || d.Contacts == nil || d.Conversations == nil ||
		d.Leads == nil || d.History == nil || d.Messages == nil || d.Media == nil || d.Assignments == nil ||
		d.Automation == nil || d.Claims == nil || d.Forwarder == nil || d.SenderIP == nil || d.Guard == nil || d.Receipts == nil ||
		d.Originals == nil || d.Transcript == nil || d.Engagement == nil || d.EmailsSeen == nil {
		panic("emailinbox: the inbound handler is missing a dependency")
	}
	return &Inbound{d: d, now: utcNow}
}

type route struct {
	workspaceID string
	domainID    string
	original    emailaddress.Address
	pinned      string
	sent        *emailsending.Message
}

func (s *Inbound) HandleInbound(ctx context.Context, notice emailsending.InboundNotice) error {
	if notice.Verdict.Rejected() {
		return s.discardRejected(ctx, notice)
	}
	raw, err := s.d.Raw.Fetch(ctx, notice.ObjectKey)
	switch {
	case errors.Is(err, emailsending.ErrRawMessageKey), errors.Is(err, emailsending.ErrRawMessageTooLarge):
		log.Printf("%s inbound %s dropped, its raw message cannot be read: %v", logPrefix, notice.MessageID, err)
		return nil
	case err != nil:
		return err
	}
	if ip, ok := s.d.SenderIP.ConnectingIP(raw); ok {
		s.d.Guard.RecordSender(ctx, ip)
	}
	msg, err := s.d.Parser.Parse(raw)
	if err != nil {
		log.Printf("%s inbound %s dropped, unreadable message: %v", logPrefix, notice.MessageID, err)
		return nil
	}
	handled := map[string]bool{}
	for _, recipient := range uniqueRecipients(notice.Recipients) {
		target, ok, err := s.resolve(ctx, recipient, msg.From.Address)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		key := target.workspaceID + "|" + target.pinned
		if handled[key] {
			continue
		}
		handled[key] = true
		if err := s.deliver(ctx, target, msg, notice, int64(len(raw))); err != nil {
			return err
		}
	}
	return nil
}

func uniqueRecipients(in []emailaddress.Address) []emailaddress.Address {
	seen := make(map[emailaddress.Address]bool, len(in))
	out := make([]emailaddress.Address, 0, len(in))
	for _, raw := range in {
		address, err := emailaddress.Normalize(string(raw))
		if err != nil || seen[address] {
			continue
		}
		seen[address] = true
		out = append(out, address)
	}
	return out
}

func (s *Inbound) resolve(ctx context.Context, recipient, from emailaddress.Address) (route, bool, error) {
	purpose, id, err := s.d.Codec.OpenAddress(recipient)
	switch {
	case err == nil && purpose == emailtoken.PurposeUnsubscribe:
		if err := s.d.Unsubscriber.UnsubscribeMailto(ctx, id); err != nil {
			if errors.Is(err, emailsending.ErrMessageNotFound) || errors.Is(err, emailconsent.ErrUnsubscribeTokenInvalid) {
				log.Printf("%s mailto unsubscribe for unknown message %s dropped", logPrefix, id)
				return route{}, false, nil
			}
			return route{}, false, err
		}
		return route{}, false, nil
	case err == nil && purpose == emailtoken.PurposeReply:
		return s.resolveReply(ctx, recipient, id)
	case errors.Is(err, emailtoken.ErrInvalidToken):
		log.Printf("%s reply token on %s does not verify, routing by its domain and sender %s only", logPrefix, recipient, from)
	}
	return s.resolveByDomain(ctx, recipient)
}

func (s *Inbound) resolveReply(ctx context.Context, recipient emailaddress.Address, messageID string) (route, bool, error) {
	sent, err := s.d.Sent.Get(ctx, messageID)
	if errors.Is(err, emailsending.ErrMessageNotFound) {
		log.Printf("%s reply token on %s names an unknown message %s, routing by its domain", logPrefix, recipient, messageID)
		return s.resolveByDomain(ctx, recipient)
	}
	if err != nil {
		return route{}, false, err
	}
	owner, err := s.d.ReplyDomains.GetByReplyDomain(ctx, recipient.Domain())
	switch {
	case err == nil && owner.WorkspaceID != sent.WorkspaceID:
		log.Printf("%s ERROR reply token of workspace %s arrived on the reply domain of workspace %s; dropped", logPrefix, sent.WorkspaceID, owner.WorkspaceID)
		return route{}, false, nil
	case err != nil && !errors.Is(err, emailsending.ErrDomainNotFound):
		return route{}, false, err
	}
	target := route{workspaceID: sent.WorkspaceID, domainID: sent.DomainID, original: sent.Address, sent: sent}
	if sent.RunKind == emailsending.RunReply {
		target.pinned = sent.RunID
	}
	return target, true, nil
}

func (s *Inbound) resolveByDomain(ctx context.Context, recipient emailaddress.Address) (route, bool, error) {
	owner, err := s.d.ReplyDomains.GetByReplyDomain(ctx, recipient.Domain())
	if errors.Is(err, emailsending.ErrDomainNotFound) {
		log.Printf("%s ERROR inbound to %s dropped: no workspace receives replies on %s", logPrefix, recipient, recipient.Domain())
		return route{}, false, nil
	}
	if err != nil {
		return route{}, false, err
	}
	return route{workspaceID: owner.WorkspaceID, domainID: owner.ID}, true, nil
}

type delivery struct {
	conv        *em.Conversation
	contact     *em.Contact
	inbox       *em.Inbox
	fromDiffers bool
	original    emailaddress.Address
}

func (s *Inbound) deliver(ctx context.Context, target route, msg em.InboundMessage, notice emailsending.InboundNotice, sizeBytes int64) error {
	contact, err := s.contactFor(ctx, target.workspaceID, msg.From)
	if err != nil {
		return err
	}
	d, ok, err := s.conversationFor(ctx, target, contact)
	if err != nil || !ok {
		return err
	}
	externalID := em.ExternalMessageID(msg.MessageID, notice.MessageID)
	if externalID == "" {
		log.Printf("%s inbound to conversation %s dropped: no message id to deduplicate it", logPrefix, d.conv.ID)
		return nil
	}
	if err := s.recordOriginal(ctx, d, target.sent); err != nil {
		return err
	}
	if err := s.chargeReceive(ctx, target.workspaceID, notice.MessageID, sizeBytes); err != nil {
		return err
	}
	recorded, err := s.record(ctx, d, msg, externalID, s.receivedAt(notice))
	if err != nil || !recorded {
		return err
	}
	s.afterRecord(ctx, d, msg, externalID, s.receivedAt(notice))
	return nil
}

func (s *Inbound) recordOriginal(ctx context.Context, d delivery, sent *emailsending.Message) error {
	if sent == nil {
		return nil
	}
	held, err := s.d.Transcript.Holds(d.conv.ID, em.OutboundExternalID(sent.ProviderMessageID, sent.ID))
	if err != nil || held {
		return err
	}
	original, found, err := s.d.Originals.Original(ctx, *sent)
	if err != nil {
		return fmt.Errorf("emailinbox: recalling the email %s that %s answers: %w", sent.ID, d.conv.ID, err)
	}
	if !found {
		log.Printf("%s the %s email %s answered in conversation %s has no stored content to show; the reply is recorded alone", logPrefix, sent.RunKind, sent.ID, d.conv.ID)
		return nil
	}
	from := ""
	if d.inbox != nil {
		from = string(d.inbox.Address)
	}
	return s.d.Transcript.TranscribeInto(ctx, *d.conv, from, original.OutboundOf(*sent))
}

func (s *Inbound) discardRejected(ctx context.Context, notice emailsending.InboundNotice) error {
	if err := s.d.Raw.Delete(ctx, notice.ObjectKey); err != nil && !errors.Is(err, emailsending.ErrRawMessageKey) {
		return fmt.Errorf("emailinbox: deleting the rejected message %s: %w", notice.MessageID, err)
	}
	log.Printf("%s inbound %s dropped and deleted: spam %q, virus %q", logPrefix, notice.MessageID, notice.Verdict.Spam, notice.Verdict.Virus)
	return nil
}

func (s *Inbound) chargeReceive(ctx context.Context, workspaceID, providerMessageID string, sizeBytes int64) error {
	charge, err := s.d.Receipts.ChargeReceive(ctx, workspaceID, providerMessageID, sizeBytes)
	if err != nil {
		return fmt.Errorf("emailinbox: charging the reply %s to workspace %s: %w", providerMessageID, workspaceID, err)
	}
	if !emailsending.InboundPaused(charge.BalanceAfterMicros) {
		return nil
	}
	if err := s.d.Guard.Pause(ctx, workspaceID); err != nil {
		log.Printf("%s ERROR replies of workspace %s stay open until the sweep pauses them: %v", logPrefix, workspaceID, err)
	}
	return nil
}

func (s *Inbound) receivedAt(notice emailsending.InboundNotice) time.Time {
	if notice.ReceivedAt.IsZero() {
		return s.now()
	}
	return notice.ReceivedAt.UTC()
}

func (s *Inbound) contactFor(ctx context.Context, workspaceID string, from em.Mailbox) (*em.Contact, error) {
	contact, err := s.d.Contacts.FindOrCreate(ctx, em.FindOrCreateContactInput{
		WorkspaceID: workspaceID, Address: from.Address, DisplayName: from.Name,
	})
	if err != nil {
		return nil, err
	}
	s.d.EmailsSeen.AddressesSeen(ctx, []emailaddress.Address{contact.Address})
	if strings.TrimSpace(contact.DisplayName) == "" && strings.TrimSpace(from.Name) != "" {
		if err := s.d.Contacts.UpdateDisplayName(ctx, contact.ID, from.Name); err != nil {
			log.Printf("%s display name of contact %s not stored: %v", logPrefix, contact.ID, err)
		} else {
			contact.DisplayName = strings.TrimSpace(from.Name)
		}
	}
	if contact.LeadID == nil {
		s.linkLead(ctx, contact)
	}
	return contact, nil
}

func (s *Inbound) linkLead(ctx context.Context, contact *em.Contact) {
	found, err := s.d.Leads.FindByEmail(contact.WorkspaceID, string(contact.Address))
	switch {
	case errors.Is(err, lead.ErrLeadNotFound), errors.Is(err, lead.ErrLeadInvalid):
		return
	case err != nil:
		log.Printf("%s lead of contact %s not looked up: %v", logPrefix, contact.ID, err)
		return
	case found == nil || found.WorkspaceID != contact.WorkspaceID:
		return
	}
	if err := s.d.Contacts.LinkLead(ctx, contact.ID, found.ID); err != nil {
		log.Printf("%s lead %s not linked to contact %s: %v", logPrefix, found.ID, contact.ID, err)
		return
	}
	leadID := found.ID
	contact.LeadID = &leadID
}

func (s *Inbound) conversationFor(ctx context.Context, target route, contact *em.Contact) (delivery, bool, error) {
	d := delivery{contact: contact, original: target.original}
	d.fromDiffers = target.original != "" && target.original != contact.Address
	if target.pinned != "" {
		pinned, keep, err := s.pinnedConversation(ctx, target, contact)
		if err != nil {
			return d, false, err
		}
		if !keep {
			return d, false, nil
		}
		if pinned != nil {
			d.conv = pinned
		}
	}
	if d.conv == nil {
		conv, ok, err := s.keyedConversation(ctx, target, contact)
		if err != nil || !ok {
			return d, false, err
		}
		d.conv = conv
	}
	d.inbox = s.inboxFor(ctx, d.conv)
	return d, true, nil
}

func (s *Inbound) pinnedConversation(ctx context.Context, target route, contact *em.Contact) (*em.Conversation, bool, error) {
	conv, err := s.d.Conversations.FindByID(ctx, target.pinned)
	if errors.Is(err, em.ErrConversationNotFound) {
		return nil, true, nil
	}
	if err != nil {
		return nil, false, err
	}
	if conv.WorkspaceID != target.workspaceID {
		log.Printf("%s ERROR reply pinned to conversation %s of another workspace; dropped", logPrefix, conv.ID)
		return nil, false, nil
	}
	if conv.ContactID == contact.ID {
		return conv, true, nil
	}
	owner, err := s.d.Contacts.FindByID(ctx, conv.ContactID)
	if err != nil && !errors.Is(err, em.ErrContactNotFound) {
		return nil, false, err
	}
	if owner == nil || owner.Address != target.original {
		log.Printf("%s reply from %s to a copy forwarded out of conversation %s never opens a conversation; dropped", logPrefix, contact.Address, conv.ID)
		return nil, false, nil
	}
	return nil, true, nil
}

func (s *Inbound) keyedConversation(ctx context.Context, target route, contact *em.Contact) (*em.Conversation, bool, error) {
	conv, err := s.d.Conversations.FindByContact(ctx, target.workspaceID, contact.ID)
	if err == nil {
		return conv, true, nil
	}
	if !errors.Is(err, em.ErrConversationNotFound) {
		return nil, false, err
	}
	senders, err := s.d.Senders.ListByWorkspace(ctx, target.workspaceID)
	if err != nil {
		return nil, false, err
	}
	sender, found := em.PickSender(senders, target.domainID)
	if !found {
		log.Printf("%s ERROR inbound for workspace %s dropped: it has no sender to own the conversation", logPrefix, target.workspaceID)
		return nil, false, nil
	}
	if _, err := s.ensureInbox(ctx, sender); err != nil {
		return nil, false, err
	}
	conv, _, err = s.d.Conversations.FindOrCreate(ctx, em.FindOrCreateConversationInput{
		WorkspaceID: target.workspaceID, ContactID: contact.ID, SenderID: sender.ID,
	})
	if err != nil {
		return nil, false, err
	}
	return conv, true, nil
}

func (s *Inbound) ensureInbox(ctx context.Context, sender emailsending.Sender) (*em.Inbox, error) {
	return ensureSenderInbox(ctx, s.d.Domains, s.d.Inboxes, sender)
}

func ensureSenderInbox(ctx context.Context, domains SendingDomains, inboxes em.InboxRepository, sender emailsending.Sender) (*em.Inbox, error) {
	domain, err := domains.Get(ctx, sender.WorkspaceID, sender.DomainID)
	if err != nil {
		return nil, err
	}
	return inboxes.Ensure(ctx, em.InboxForSender(sender, *domain))
}

func (s *Inbound) inboxFor(ctx context.Context, conv *em.Conversation) *em.Inbox {
	sender, err := s.d.Senders.Get(ctx, conv.WorkspaceID, conv.SenderID)
	if err == nil {
		if inbox, err := s.ensureInbox(ctx, *sender); err == nil {
			return inbox
		}
	}
	inbox, err := s.d.Inboxes.FindByIDUnscoped(ctx, conv.SenderID)
	if err != nil {
		log.Printf("%s inbox %s of conversation %s unreadable, no automation runs: %v", logPrefix, conv.SenderID, conv.ID, err)
		return &em.Inbox{ID: conv.SenderID, WorkspaceID: conv.WorkspaceID}
	}
	return inbox
}

type inboundRow struct {
	ref         string
	messageType conversation.MessageType
	text        string
	media       *conversation.ConversationMedia
	extra       map[string]any
}

func (s *Inbound) record(ctx context.Context, d delivery, msg em.InboundMessage, externalID string, at time.Time) (bool, error) {
	if _, err := s.d.Messages.GetByEntryAndExternalMessageID(shared.EntryTypeEmail, d.conv.ID, externalID); err == nil {
		return false, nil
	} else if !errors.Is(err, conversation.ErrMessageNotFound) {
		return false, err
	}
	if err := s.d.Conversations.ApplyThread(ctx, d.conv.ID, d.conv.ThreadAfter(msg)); err != nil {
		return false, err
	}
	if err := s.d.Conversations.RecordInbound(ctx, d.conv.ID, at); err != nil {
		return false, err
	}
	s.d.Assignments.EnsureAssignment(d.conv.ID, string(shared.EntryTypeEmail), d.conv.SenderID)

	base := baseMetadata(d, msg, externalID)
	rows := s.rows(ctx, d.conv, msg, externalID)
	if id := storeHTML(s.d.Media, d.conv.ID, msg.HTML); id != "" && len(rows) > 0 {
		if rows[0].extra == nil {
			rows[0].extra = map[string]any{}
		}
		rows[0].extra[em.MetadataHTMLMediaID] = id
	}
	for _, row := range rows {
		if err := s.write(ctx, d, msg, at, base, row); err != nil {
			return false, err
		}
	}
	return true, nil
}

func baseMetadata(d delivery, msg em.InboundMessage, externalID string) map[string]any {
	meta := map[string]any{em.MetadataMessageID: externalID}
	if subject := strings.TrimSpace(msg.Subject); subject != "" {
		meta[em.MetadataSubject] = subject
	}
	if d.fromDiffers {
		meta[em.MetadataFromDiffers] = true
		meta[em.MetadataOriginalRecipient] = string(d.original)
	}
	if msg.Automated {
		meta[em.MetadataAutomated] = true
	}
	return meta
}

func (s *Inbound) rows(ctx context.Context, conv *em.Conversation, msg em.InboundMessage, externalID string) []inboundRow {
	attachments := msg.Attachments
	if len(attachments) > em.MaxAttachments {
		log.Printf("%s %d attachments of %s beyond the first %d were not kept", logPrefix, len(attachments)-em.MaxAttachments, externalID, em.MaxAttachments)
		attachments = attachments[:em.MaxAttachments]
	}
	var rows []inboundRow
	next := 0
	refFor := func() string {
		defer func() { next++ }()
		if next == 0 {
			return externalID
		}
		return em.AttachmentRef(externalID, next-1)
	}
	if strings.TrimSpace(msg.Text) != "" || len(attachments) == 0 {
		text := msg.Text
		extra := map[string]any{}
		if strings.TrimSpace(text) == "" {
			text = em.EmptyBodyText
		}
		text, cut := em.TruncateText(text, em.MaxTranscriptTextBytes)
		if cut {
			extra[em.MetadataTruncated] = true
		}
		if quoted, _ := em.TruncateText(msg.QuotedText, em.MaxTranscriptTextBytes); quoted != "" {
			extra[em.MetadataQuotedText] = quoted
		}
		rows = append(rows, inboundRow{ref: refFor(), messageType: conversation.MessageTypeUserMessage, text: text, extra: extra})
	}
	kept := em.AttachmentsWithinBudget(attachments)
	for i, att := range attachments {
		ref := refFor()
		stored, err := s.store(conv, att, kept[i])
		if err != nil {
			log.Printf("%s attachment %q of %s kept without its file: %v", logPrefix, att.FileName, externalID, err)
			rows = append(rows, inboundRow{ref: ref, messageType: conversation.MessageTypeUnsupported, text: em.UnavailableAttachmentText,
				extra: map[string]any{em.MetadataAttachmentUnavailable: true, em.MetadataFileName: att.FileName}})
			continue
		}
		rows = append(rows, inboundRow{ref: ref, messageType: conversation.MessageTypeMedia, media: stored,
			extra: map[string]any{em.MetadataFileName: att.FileName}})
	}
	return rows
}

var errAttachmentOverBudget = errors.New("emailinbox: attachment beyond the kept bytes of one email")

func (s *Inbound) store(conv *em.Conversation, att em.Attachment, withinBudget bool) (*conversation.ConversationMedia, error) {
	if !withinBudget {
		return nil, errAttachmentOverBudget
	}
	contentType := strings.TrimSpace(att.ContentType)
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	id := uuid.NewString()
	return s.d.Media.Store(conversation.StoreMediaInput{
		ID:               id,
		Key:              fmt.Sprintf("conversations/%s/%s/%s%s", shared.EntryTypeEmail, conv.ID, id, attachmentExtension(att.FileName, contentType)),
		EntryID:          conv.ID,
		EntryType:        shared.EntryTypeEmail,
		Type:             mediaTypeOf(contentType),
		MimeType:         contentType,
		Data:             att.Data,
		OriginalFilename: att.FileName,
	})
}

func mediaTypeOf(contentType string) conversation.MediaType {
	switch {
	case strings.HasPrefix(contentType, "image/"):
		return conversation.MediaTypeImage
	case strings.HasPrefix(contentType, "video/"):
		return conversation.MediaTypeVideo
	case strings.HasPrefix(contentType, "audio/"):
		return conversation.MediaTypeAudio
	}
	return conversation.MediaTypeDocument
}

func attachmentExtension(name, contentType string) string {
	if ext := path.Ext(name); ext != "" && len(ext) <= 10 && !strings.ContainsAny(ext, `/\ `) {
		return strings.ToLower(ext)
	}
	if exts, err := mime.ExtensionsByType(contentType); err == nil && len(exts) > 0 {
		return exts[0]
	}
	return ""
}

func (s *Inbound) write(ctx context.Context, d delivery, msg em.InboundMessage, at time.Time, base map[string]any, row inboundRow) error {
	meta := make(map[string]any, len(base)+len(row.extra))
	for k, v := range base {
		meta[k] = v
	}
	for k, v := range row.extra {
		meta[k] = v
	}
	raw, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	to := ""
	if d.inbox != nil {
		to = string(d.inbox.Address)
	}
	rec := conversation.MessageHistoryRecord{
		SentBy:            conversation.SentByContact(string(d.contact.Address)),
		EntryID:           d.conv.ID,
		EntryType:         shared.EntryTypeEmail,
		Channel:           conversation.MessageChannelEmail,
		MessageType:       row.messageType,
		ProviderMessageID: row.ref,
		From:              string(msg.From.Address),
		To:                to,
		Text:              row.text,
		Timestamp:         at,
		Metadata:          raw,
		SenderName:        d.contact.Name(),
	}
	if row.media != nil {
		rec.MediaID, rec.MediaType, rec.MediaURL = row.media.ID, row.media.Type, row.media.URL
	}
	return s.d.History.Record(ctx, rec)
}

func (s *Inbound) afterRecord(ctx context.Context, d delivery, msg em.InboundMessage, externalID string, at time.Time) {
	claimed, err := s.d.Claims.Claim(ctx, dispatchClaimKey(d.conv.ID, externalID), dispatchClaimChannel, d.conv.SenderID)
	switch {
	case err != nil:
		log.Printf("%s automation of %s skipped, its claim on %s could not be written: %v", logPrefix, d.conv.ID, externalID, err)
		return
	case !claimed:
		return
	}
	if msg.Automated {
		log.Printf("%s %s is an automatic reply; recorded without automation or forward", logPrefix, externalID)
		return
	}
	if err := s.d.Engagement.Replied(ctx, d.conv.WorkspaceID, msg.From.Address, at); err != nil {
		log.Printf("%s the reply %s was not counted as engagement of %s: %v", logPrefix, externalID, msg.From.Address, err)
	}
	config := conversationAutomationConfig(d.inbox)
	if err := s.d.Automation.Dispatch(ctx, conversation_usecase.InboundAutomationInput{
		WorkspaceID:          d.conv.WorkspaceID,
		EntryID:              d.conv.ID,
		EntryType:            shared.EntryTypeEmail,
		ContactRef:           string(d.contact.Address),
		ProviderMessageID:    externalID,
		ReceivedAt:           at,
		LeadID:               d.contact.LeadID,
		Text:                 msg.Text,
		ConversationOverride: d.conv.AutomationEnabled,
		Config:               config,
	}); err != nil {
		log.Printf("%s automation of %s failed: %v", logPrefix, d.conv.ID, err)
	}
	s.d.Forwarder.Forward(ctx, ForwardInput{Conversation: d.conv, Contact: d.contact, Message: msg, ExternalID: externalID, ReceivedAt: at})
}

func conversationAutomationConfig(inbox *em.Inbox) conversation.ChannelAutomation {
	if inbox == nil {
		return conversation.ChannelAutomation{}
	}
	return inbox.Automation()
}
