package mercadolivre

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"mime"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"vozko/domain/conversation"
	mldomain "vozko/domain/mercadolivre"
	"vozko/domain/shared"
	conversation_usecase "vozko/usecases/conversation"
	"vozko/usecases/messagestate"
)

const (
	MediaUnavailableMetadataKey = "mercadolivre_media_unavailable"
	fromAgentMetadataKey        = "mercadolivre_from_agent"
	messageIDMetadataKey        = "mercadolivre_message_id"
	unavailableMediaText        = "[anexo indisponível]"
	maxThreadPages              = 20
	dispatchClaimChannel        = "mercadolivre-inbound-dispatch"
)

func dispatchClaimKey(conversationID, providerRef string) string {
	return "ml:dispatch:" + conversationID + ":" + providerRef
}

var (
	errRetryWithFreshToken = errors.New("mercadolivre: the access token was refreshed, retry")
	ErrResourceGone        = errors.New("mercadolivre: the resource no longer exists at Mercado Livre")
)

type ThreadSyncDeps struct {
	Contacts      mldomain.ContactRepository
	Conversations mldomain.ConversationRepository
	Services      Services
	Tokens        *TokenSource
	History       conversation.MessageHistoryManager
	Messages      MessageStore
	Media         conversation.MediaStore
	Watermarks    conversation.DeliveryWatermarkRepository
	Assignments   AssignmentService
	Automation    InboundDispatcher
	Dispatches    InboundClaims
	Announcer     messagestate.Announcer
	PackSummaries *PackSummaries
	Metrics       Metrics
}

type ThreadSync struct {
	contacts      mldomain.ContactRepository
	conversations mldomain.ConversationRepository
	svc           Services
	tokens        *TokenSource
	history       conversation.MessageHistoryManager
	messages      MessageStore
	media         conversation.MediaStore
	watermarks    conversation.DeliveryWatermarkRepository
	assignments   AssignmentService
	automation    InboundDispatcher
	dispatches    InboundClaims
	announcer     messagestate.Announcer
	summaries     *PackSummaries
	metrics       Metrics
	now           Clock
}

func NewThreadSync(d ThreadSyncDeps) *ThreadSync {
	if d.Contacts == nil || d.Conversations == nil || d.Tokens == nil || d.History == nil || d.Messages == nil ||
		d.Media == nil || d.Watermarks == nil || d.Assignments == nil || d.Automation == nil || d.Dispatches == nil || d.Announcer == nil ||
		d.PackSummaries == nil || d.Metrics == nil ||
		d.Services.Messages == nil || d.Services.Attachments == nil || d.Services.Users == nil ||
		d.Services.Packs == nil || d.Services.Orders == nil || d.Services.Errors == nil {
		panic("mercadolivre: the thread sync is missing a dependency")
	}
	return &ThreadSync{
		contacts: d.Contacts, conversations: d.Conversations, svc: d.Services, tokens: d.Tokens,
		history: d.History, messages: d.Messages, media: d.Media, watermarks: d.Watermarks,
		assignments: d.Assignments, automation: d.Automation, dispatches: d.Dispatches, announcer: d.Announcer,
		summaries: d.PackSummaries, metrics: d.Metrics, now: utcNow,
	}
}

func (s *ThreadSync) Handle(ctx context.Context, account *mldomain.Account, job mldomain.Job) error {
	creds, err := s.credentials(ctx, account)
	if err != nil {
		return err
	}
	switch job.Kind {
	case mldomain.JobFetchMessage:
		return s.syncMessage(ctx, account, creds, job)
	case mldomain.JobFetchThread:
		_, err := s.syncThread(ctx, account, creds, job.ResourceID)
		return err
	}
	return fmt.Errorf("%w: %s", ErrJobKindNotHandled, job.Kind)
}

func (s *ThreadSync) credentials(ctx context.Context, account *mldomain.Account) (mldomain.Credentials, error) {
	creds, err := s.tokens.Credentials(ctx, account)
	if errors.Is(err, mldomain.ErrNeedsReconnect) {
		return mldomain.Credentials{}, fmt.Errorf("%w: %v", ErrAccountUnavailable, err)
	}
	return creds, err
}

func (s *ThreadSync) apiErr(ctx context.Context, account *mldomain.Account, err error) error {
	return classifyAPIError(ctx, s.svc.Errors, s.tokens, s.metrics, account, err)
}

func classifyAPIError(ctx context.Context, errs mldomain.ErrorClassifier, tokens *TokenSource, metrics Metrics, account *mldomain.Account, err error) error {
	switch {
	case err == nil:
		return nil
	case errs.IsAuth(err):
		lost, recoverErr := tokens.RecoverFromAuthFailure(ctx, account)
		if recoverErr != nil {
			return recoverErr
		}
		if lost {
			return fmt.Errorf("%w: %v", ErrAccountUnavailable, err)
		}
		return fmt.Errorf("%w: %v", errRetryWithFreshToken, err)
	case errs.IsNotFound(err):
		return fmt.Errorf("%w: %v", ErrResourceGone, err)
	case errs.IsPermission(err):
		metrics.AddMercadoLivreEvent("consume", "permission_denied")
		return fmt.Errorf("%w: %v", ErrAccountUnavailable, err)
	}
	return err
}

func (s *ThreadSync) syncMessage(ctx context.Context, account *mldomain.Account, creds mldomain.Credentials, job mldomain.Job) error {
	msg, err := s.svc.Messages.GetMessage(ctx, creds, job.ResourceID)
	if err != nil {
		return s.apiErr(ctx, account, err)
	}
	if !mldomain.ValidNumericID(msg.PackKey) {
		return fmt.Errorf("%w: message %s carries no pack", mldomain.ErrInvalidResource, msg.ID)
	}
	thread, err := s.svc.Messages.Thread(ctx, creds, mldomain.ThreadQuery{PackKey: msg.PackKey, Limit: 1})
	if err != nil {
		return s.apiErr(ctx, account, err)
	}
	conv, err := s.sync(ctx, account, creds, msg.PackKey, thread, []*mldomain.RemoteMessage{msg})
	if err != nil {
		return err
	}
	if job.Read {
		return s.markRead(conv, msg)
	}
	return nil
}

func (s *ThreadSync) syncThread(ctx context.Context, account *mldomain.Account, creds mldomain.Credentials, packKey string) (*mldomain.Conversation, error) {
	var first *mldomain.Thread
	var messages []*mldomain.RemoteMessage
	offset := 0
	for page := 0; page < maxThreadPages; page++ {
		thread, err := s.svc.Messages.Thread(ctx, creds, mldomain.ThreadQuery{PackKey: packKey, Offset: offset, Limit: mldomain.MaxThreadPageLimit})
		if err != nil {
			return nil, s.apiErr(ctx, account, err)
		}
		if first == nil {
			first = thread
		}
		messages = append(messages, thread.Messages...)
		offset += len(thread.Messages)
		if len(thread.Messages) == 0 || offset >= thread.Total {
			break
		}
	}
	return s.sync(ctx, account, creds, packKey, first, messages)
}

func (s *ThreadSync) sync(ctx context.Context, account *mldomain.Account, creds mldomain.Credentials, packKey string, thread *mldomain.Thread, messages []*mldomain.RemoteMessage) (*mldomain.Conversation, error) {
	conv, contact, err := s.ensureConversation(ctx, account, creds, packKey, messages)
	if err != nil {
		return nil, err
	}
	if err := s.applyState(ctx, account, conv, thread, messages); err != nil {
		return nil, err
	}
	sorted := append([]*mldomain.RemoteMessage(nil), messages...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].CreatedAt.Before(sorted[j].CreatedAt) })

	var newest *mldomain.RemoteMessage
	newestRef := ""
	for _, msg := range sorted {
		if msg == nil || msg.ID == "" {
			continue
		}
		if msg.FromUserID == account.MLUserID {
			if err := s.reconcileOwn(ctx, conv, msg); err != nil {
				return nil, err
			}
			continue
		}
		ref, recorded, err := s.recordInbound(ctx, account, creds, conv, contact, msg)
		if err != nil {
			return nil, err
		}
		if recorded {
			newest, newestRef = msg, ref
		}
	}
	if newest != nil && s.claimDispatch(ctx, account, conv, newestRef) {
		s.metrics.AddMercadoLivreEvent("consume", "inbound_recorded")
		if err := s.automation.Dispatch(ctx, conversation_usecase.InboundAutomationInput{
			WorkspaceID:          account.WorkspaceID,
			EntryID:              conv.ID,
			EntryType:            shared.EntryTypeMercadoLivre,
			ContactRef:           contact.MLUserID,
			ProviderMessageID:    newestRef,
			ReceivedAt:           newest.CreatedAt,
			LeadID:               contact.LeadID,
			Text:                 newest.Text,
			ConversationOverride: conv.AutomationEnabled,
			Config:               account.Automation(),
		}); err != nil {
			log.Printf("[mercadolivre][notify] automation of %s failed: %v", conv.ID, err)
		}
	}
	return conv, nil
}

func (s *ThreadSync) claimDispatch(ctx context.Context, account *mldomain.Account, conv *mldomain.Conversation, providerRef string) bool {
	claimed, err := s.dispatches.Claim(ctx, dispatchClaimKey(conv.ID, providerRef), dispatchClaimChannel, account.ID)
	switch {
	case err != nil:
		s.metrics.AddMercadoLivreEvent("consume", "dispatch_claim_failed")
		log.Printf("[mercadolivre][notify] automation of %s skipped, its claim on %s could not be written: %v", conv.ID, providerRef, err)
		return false
	case !claimed:
		s.metrics.AddMercadoLivreEvent("consume", "dispatch_duplicate")
		return false
	}
	return true
}

func (s *ThreadSync) ensureConversation(ctx context.Context, account *mldomain.Account, creds mldomain.Credentials, packKey string, messages []*mldomain.RemoteMessage) (*mldomain.Conversation, *mldomain.Contact, error) {
	conv, err := s.conversations.FindByPack(ctx, account.ID, packKey)
	if err == nil {
		contact, err := s.contacts.FindByID(ctx, conv.ContactID)
		if err != nil {
			return nil, nil, err
		}
		s.refreshNickname(ctx, creds, contact)
		return conv, contact, nil
	}
	if !errors.Is(err, mldomain.ErrConversationNotFound) {
		return nil, nil, err
	}
	buyer := buyerOf(messages, account)
	if buyer == "" {
		if buyer, err = s.buyerFromSale(ctx, account, creds, packKey); err != nil {
			return nil, nil, err
		}
	}
	contact, err := s.contacts.FindOrCreateByUserID(ctx, mldomain.FindOrCreateContactInput{
		WorkspaceID: account.WorkspaceID, AccountID: account.ID, MLUserID: buyer,
	})
	if err != nil {
		return nil, nil, err
	}
	s.refreshNickname(ctx, creds, contact)
	conv, created, err := s.conversations.FindOrCreateByPack(ctx, mldomain.FindOrCreateConversationInput{
		WorkspaceID: account.WorkspaceID, AccountID: account.ID, ContactID: contact.ID, PackKey: packKey, BuyerMLUserID: buyer,
	})
	if err != nil {
		return nil, nil, err
	}
	if created {
		s.metrics.AddMercadoLivreEvent("consume", "conversation_created")
		if err := s.summaries.Refresh(ctx, creds, conv); err != nil {
			log.Printf("[mercadolivre][notify] pack summary of %s not refreshed: %v", conv.ID, err)
		}
	}
	return conv, contact, nil
}

func buyerOf(messages []*mldomain.RemoteMessage, account *mldomain.Account) string {
	for _, m := range messages {
		if m == nil {
			continue
		}
		for _, candidate := range []string{m.FromUserID, m.ToUserID} {
			if mldomain.ValidNumericID(candidate) && candidate != account.MLUserID && !mldomain.IsAgentUserID(account.SiteID, candidate) {
				return candidate
			}
		}
	}
	return ""
}

func (s *ThreadSync) buyerFromSale(ctx context.Context, account *mldomain.Account, creds mldomain.Credentials, packKey string) (string, error) {
	pack, err := s.svc.Packs.GetPack(ctx, creds, packKey)
	if err == nil && mldomain.ValidNumericID(pack.BuyerID) {
		return pack.BuyerID, nil
	}
	if err != nil && !s.svc.Errors.IsNotFound(err) {
		return "", s.apiErr(ctx, account, err)
	}
	order, err := s.svc.Orders.GetOrder(ctx, creds, packKey)
	if err != nil {
		return "", s.apiErr(ctx, account, err)
	}
	if !mldomain.ValidNumericID(order.BuyerID) {
		return "", fmt.Errorf("%w: pack %s", mldomain.ErrBuyerUnknown, packKey)
	}
	return order.BuyerID, nil
}

func (s *ThreadSync) refreshNickname(ctx context.Context, creds mldomain.Credentials, contact *mldomain.Contact) {
	refreshNickname(ctx, s.svc.Users, s.contacts, s.now(), creds, contact)
}

func refreshNickname(ctx context.Context, users mldomain.UserService, contacts mldomain.ContactRepository, now time.Time, creds mldomain.Credentials, contact *mldomain.Contact) {
	if !contact.ProfileIsStale(now) {
		return
	}
	user, err := users.GetUser(ctx, creds, contact.MLUserID)
	if err != nil {
		log.Printf("[mercadolivre][notify] nickname of %s not refreshed: %v", contact.MLUserID, err)
		return
	}
	if err := contacts.UpdateNickname(ctx, contact.ID, user.Nickname, now); err != nil {
		log.Printf("[mercadolivre][notify] nickname of %s not stored: %v", contact.MLUserID, err)
		return
	}
	contact.Nickname = strings.TrimSpace(user.Nickname)
}

func (s *ThreadSync) applyState(ctx context.Context, account *mldomain.Account, conv *mldomain.Conversation, thread *mldomain.Thread, messages []*mldomain.RemoteMessage) error {
	if thread == nil {
		return nil
	}
	mediated := mldomain.MediationFromPath(thread.Path)
	if mediated == nil {
		for _, m := range messages {
			if m != nil && mldomain.IsAgentUserID(account.SiteID, m.FromUserID) {
				yes := true
				mediated = &yes
				break
			}
		}
	}
	if mediated == nil {
		mediated = conv.AgentMediated
	}
	state, err := mldomain.NewThreadState(thread.Status, thread.Substatus, mediated, s.now())
	if err != nil {
		s.metrics.AddMercadoLivreEvent("thread", "unknown_status")
		log.Printf("[mercadolivre][notify] conversation %s closed on an unreadable thread state (%q, %q): %v", conv.ID, thread.Status, thread.Substatus, err)
		state.WindowOpen = false
		state.OutboundBudget = 0
	}
	if err := s.conversations.ApplyStatus(ctx, conv.ID, state); err != nil {
		return err
	}
	conv.MLStatus, conv.MLSubstatus, conv.AgentMediated = state.Status, state.Substatus, state.AgentMediated
	conv.OutboundBudget, conv.WindowOpen = state.OutboundBudget, state.WindowOpen
	return nil
}

func (s *ThreadSync) recordInbound(ctx context.Context, account *mldomain.Account, creds mldomain.Credentials,
	conv *mldomain.Conversation, contact *mldomain.Contact, msg *mldomain.RemoteMessage) (string, bool, error) {
	text := msg.Text
	refs := providerRefs(msg.ID, text, len(msg.Attachments))
	if _, err := s.messages.GetByEntryAndExternalMessageID(shared.EntryTypeMercadoLivre, conv.ID, refs[0]); err == nil {
		return refs[0], false, nil
	} else if !errors.Is(err, conversation.ErrMessageNotFound) {
		return "", false, err
	}
	if err := s.conversations.RecordInbound(ctx, conv.ID, msg.CreatedAt); err != nil {
		return "", false, err
	}
	s.assignments.EnsureAssignment(conv.ID, string(shared.EntryTypeMercadoLivre), account.ID)

	meta := map[string]any{messageIDMetadataKey: msg.ID}
	if mldomain.IsAgentUserID(account.SiteID, msg.FromUserID) {
		meta[fromAgentMetadataKey] = true
	}
	base := inboundRecord{conv: conv, contact: contact, from: msg.FromUserID, to: account.MLUserID, at: msg.CreatedAt, meta: meta}

	next := 0
	if text != "" || len(msg.Attachments) == 0 {
		messageType := conversation.MessageTypeUserMessage
		if text == "" {
			messageType, text = conversation.MessageTypeUnsupported, unavailableMediaText
		}
		if err := s.record(ctx, base.with(refs[next], messageType, text, nil, nil)); err != nil {
			return "", false, err
		}
		next++
	}
	for i, att := range msg.Attachments {
		ref := refs[next+i]
		stored, err := s.storeAttachment(ctx, creds, conv, att)
		if err != nil {
			s.metrics.AddMercadoLivreEvent("consume", "media_unavailable")
			log.Printf("[mercadolivre][notify] attachment %s of %s kept without its file: %v", att.ID, conv.ID, err)
			if err := s.record(ctx, base.with(ref, conversation.MessageTypeUnsupported, unavailableMediaText, nil,
				map[string]any{MediaUnavailableMetadataKey: true, "mercadolivre_file_name": att.OriginalName})); err != nil {
				return "", false, err
			}
			continue
		}
		if err := s.record(ctx, base.with(ref, conversation.MessageTypeMedia, "", stored, nil)); err != nil {
			return "", false, err
		}
	}
	return refs[0], true, nil
}

func providerRefs(messageID, text string, attachments int) []string {
	if attachments == 0 || (attachments == 1 && text == "") {
		return []string{messageID}
	}
	refs := make([]string, 0, attachments+1)
	if text != "" {
		refs = append(refs, messageID)
	}
	for i := 0; i < attachments; i++ {
		refs = append(refs, messageID+":att"+strconv.Itoa(i))
	}
	return refs
}

type inboundRecord struct {
	conv    *mldomain.Conversation
	contact *mldomain.Contact
	from    string
	to      string
	at      time.Time
	meta    map[string]any

	ref         string
	messageType conversation.MessageType
	text        string
	media       *conversation.ConversationMedia
	extra       map[string]any
}

func (r inboundRecord) with(ref string, messageType conversation.MessageType, text string, media *conversation.ConversationMedia, extra map[string]any) inboundRecord {
	r.ref, r.messageType, r.text, r.media, r.extra = ref, messageType, text, media, extra
	return r
}

func (s *ThreadSync) record(ctx context.Context, r inboundRecord) error {
	meta := make(map[string]any, len(r.meta)+len(r.extra))
	for k, v := range r.meta {
		meta[k] = v
	}
	for k, v := range r.extra {
		meta[k] = v
	}
	raw, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	rec := conversation.MessageHistoryRecord{
		SentBy:            conversation.SentByContact(r.from),
		EntryID:           r.conv.ID,
		EntryType:         shared.EntryTypeMercadoLivre,
		Channel:           conversation.MessageChannelMercadoLivre,
		MessageType:       r.messageType,
		ProviderMessageID: r.ref,
		From:              r.from,
		To:                r.to,
		Text:              r.text,
		Timestamp:         r.at,
		Metadata:          raw,
		SenderName:        r.contact.DisplayName(),
	}
	if r.media != nil {
		rec.MediaID, rec.MediaType, rec.MediaURL = r.media.ID, r.media.Type, r.media.URL
	}
	return s.history.Record(ctx, rec)
}

func (s *ThreadSync) storeAttachment(ctx context.Context, creds mldomain.Credentials, conv *mldomain.Conversation, att mldomain.RemoteAttachment) (*conversation.ConversationMedia, error) {
	file, err := s.svc.Attachments.Download(ctx, creds, att.ID)
	if err != nil {
		return nil, err
	}
	contentType := strings.TrimSpace(att.ContentType)
	if contentType == "" {
		contentType = file.ContentType
	}
	mediaType := conversation.MediaTypeDocument
	if strings.HasPrefix(contentType, "image/") {
		mediaType = conversation.MediaTypeImage
	}
	id := uuid.NewString()
	return s.media.Store(conversation.StoreMediaInput{
		ID:               id,
		Key:              fmt.Sprintf("conversations/%s/%s/%s%s", shared.EntryTypeMercadoLivre, conv.ID, id, attachmentExtension(att.OriginalName, contentType)),
		EntryID:          conv.ID,
		EntryType:        shared.EntryTypeMercadoLivre,
		Type:             mediaType,
		MimeType:         contentType,
		Data:             file.Data,
		OriginalFilename: att.OriginalName,
	})
}

func attachmentExtension(name, contentType string) string {
	if ext := path.Ext(name); ext != "" {
		return ext
	}
	if exts, err := mime.ExtensionsByType(contentType); err == nil && len(exts) > 0 {
		return exts[0]
	}
	return ""
}

func (s *ThreadSync) reconcileOwn(ctx context.Context, conv *mldomain.Conversation, msg *mldomain.RemoteMessage) error {
	moderation := moderationValue(msg.Status, msg.ModerationStatus, msg.ModerationReason)
	existing, err := s.messages.GetByEntryAndExternalMessageID(shared.EntryTypeMercadoLivre, conv.ID, msg.ID)
	if err == nil {
		if sameModeration(storedModeration(existing), moderation) {
			return nil
		}
		if err := existing.AttachMetadata(ModerationMetadataKey, moderation); err != nil {
			return err
		}
		if moderationFailed(msg.Status, msg.ModerationStatus) {
			existing.DeliveryStatus = conversation.DeliveryStatusFailed
			s.metrics.AddMercadoLivreEvent("moderation", "rejected")
		}
		return messagestate.Writer{Messages: s.messages, Announcer: s.announcer}.Save(existing)
	}
	if !errors.Is(err, conversation.ErrMessageNotFound) {
		return err
	}
	raw, err := json.Marshal(map[string]any{messageIDMetadataKey: msg.ID, ModerationMetadataKey: moderation})
	if err != nil {
		return err
	}
	return s.history.Record(ctx, conversation.MessageHistoryRecord{
		SentBy:            conversation.SentExternally(),
		EntryID:           conv.ID,
		EntryType:         shared.EntryTypeMercadoLivre,
		Channel:           conversation.MessageChannelMercadoLivre,
		MessageType:       conversation.MessageTypeOperator,
		ProviderMessageID: msg.ID,
		From:              msg.FromUserID,
		To:                msg.ToUserID,
		Text:              msg.Text,
		Timestamp:         msg.CreatedAt,
		Metadata:          raw,
	})
}

func (s *ThreadSync) markRead(conv *mldomain.Conversation, msg *mldomain.RemoteMessage) error {
	at := s.now()
	if msg.ReadAt != nil {
		at = *msg.ReadAt
	}
	_, err := s.watermarks.MarkOutboundStatusUpTo(conv.ID, shared.EntryTypeMercadoLivre, conversation.DeliveryStatusRead, at)
	return err
}

func (s *ThreadSync) RefreshState(ctx context.Context, account *mldomain.Account, conv *mldomain.Conversation) error {
	creds, err := s.credentials(ctx, account)
	if err != nil {
		return err
	}
	thread, err := s.svc.Messages.Thread(ctx, creds, mldomain.ThreadQuery{PackKey: conv.PackKey, Limit: 1})
	if err != nil {
		return s.apiErr(ctx, account, err)
	}
	return s.applyState(ctx, account, conv, thread, nil)
}

func (s *ThreadSync) ApplyBlock(ctx context.Context, account *mldomain.Account, conv *mldomain.Conversation, substatus string) error {
	if !mldomain.KnownSubstatus(substatus) {
		return s.RefreshState(ctx, account, conv)
	}
	return s.applyState(ctx, account, conv, &mldomain.Thread{Status: mldomain.ThreadStatusBlocked, Substatus: substatus}, nil)
}
