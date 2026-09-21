package instagram

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
	igdomain "vozko/domain/instagram"
	"vozko/domain/media"
	"vozko/domain/shared"
	"vozko/domain/workflow"
)

const profileTTL = 7 * 24 * time.Hour

type AssignmentService interface {
	EnsureAssignment(entryID, entryType, accountID string) string
}

type AIReplier interface {
	Reply(ctx context.Context, req conversation.AIReplyRequest) (*conversation.Message, error)
}

type WorkflowTrigger interface {
	Evaluate(event workflow.TriggerEvent)
}

type AnalysisScheduler interface {
	ScheduleAnalysis(entryID string, entryType shared.EntryType)
}

type CommentRuleEvaluator interface {
	Execute(ctx context.Context, comment *igdomain.Comment)
}

type AudienceEnqueuer interface {
	Enqueue(ctx context.Context, comment *igdomain.Comment)
	Forget(ctx context.Context, igCommentID string)
}

type MediaFetcher interface {
	FetchMediaBytes(ctx context.Context, url string) (data []byte, contentType string, err error)
}

type HandleWebhookUseCase struct {
	accounts      igdomain.AccountRepository
	contacts      igdomain.ContactRepository
	conversations igdomain.ConversationRepository
	comments      igdomain.CommentRepository
	mediaRepo     igdomain.MediaRepository
	messaging     igdomain.MessagingService
	mediaFetcher  MediaFetcher

	history      conversation.MessageHistoryManager
	messages     conversation.MessageRepository
	convMedia    conversation.ConversationMediaRepository
	fileStorage  media.FileStorage
	broadcaster  conversation.EventBroadcaster
	assignments  AssignmentService
	aiReply      AIReplier
	workflows    WorkflowTrigger
	commentRules CommentRuleEvaluator
	analysis     AnalysisScheduler
	audience     AudienceEnqueuer
}

type HandleWebhookDeps struct {
	Accounts      igdomain.AccountRepository
	Contacts      igdomain.ContactRepository
	Conversations igdomain.ConversationRepository
	Comments      igdomain.CommentRepository
	Media         igdomain.MediaRepository
	Messaging     igdomain.MessagingService
	MediaFetcher  MediaFetcher

	History      conversation.MessageHistoryManager
	Messages     conversation.MessageRepository
	ConvMedia    conversation.ConversationMediaRepository
	FileStorage  media.FileStorage
	Broadcaster  conversation.EventBroadcaster
	Assignments  AssignmentService
	AIReply      AIReplier
	Workflows    WorkflowTrigger
	CommentRules CommentRuleEvaluator
	Analysis     AnalysisScheduler
	Audience     AudienceEnqueuer
}

func NewHandleWebhookUseCase(d HandleWebhookDeps) *HandleWebhookUseCase {
	return &HandleWebhookUseCase{
		accounts:      d.Accounts,
		contacts:      d.Contacts,
		conversations: d.Conversations,
		comments:      d.Comments,
		mediaRepo:     d.Media,
		messaging:     d.Messaging,
		mediaFetcher:  d.MediaFetcher,
		history:       d.History,
		messages:      d.Messages,
		convMedia:     d.ConvMedia,
		fileStorage:   d.FileStorage,
		broadcaster:   d.Broadcaster,
		assignments:   d.Assignments,
		aiReply:       d.AIReply,
		workflows:     d.Workflows,
		commentRules:  d.CommentRules,
		analysis:      d.Analysis,
		audience:      d.Audience,
	}
}

var ErrUnknownAccount = errors.New("instagram: webhook for an unknown account")

func (uc *HandleWebhookUseCase) Execute(ctx context.Context, env *igdomain.EntryEnvelope) error {
	events := igdomain.NormalizeEntry(env)
	if len(events) == 0 {
		return nil
	}

	kinds := make([]string, 0, len(events))
	for _, ev := range events {
		kinds = append(kinds, string(ev.Kind))
	}
	log.Printf("[instagram] entry account=%s normalized into %d event(s): %v",
		env.Entry.ID, len(events), kinds)

	account, err := uc.resolveAccount(ctx, events)
	if err != nil {
		log.Printf("[instagram] cannot resolve account for entry %s: %v", env.Entry.ID, err)
		return err
	}

	var firstErr error
	for _, ev := range events {
		if err := uc.handleEvent(ctx, account, ev); err != nil {
			log.Printf("[instagram] event handling failed kind=%s account=%s: %v",
				ev.Kind, account.IGUserID, err)
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

func (uc *HandleWebhookUseCase) resolveAccount(ctx context.Context, events []*igdomain.Event) (*igdomain.Account, error) {
	for _, ev := range events {
		if ev.IGAccountExternalID == "" {
			continue
		}
		account, err := uc.accounts.FindByIGUserID(ctx, ev.IGAccountExternalID)
		if err == nil {
			return account, nil
		}
		if !errors.Is(err, igdomain.ErrAccountNotFound) {
			return nil, err
		}
	}
	return nil, ErrUnknownAccount
}

func (uc *HandleWebhookUseCase) handleEvent(ctx context.Context, account *igdomain.Account, ev *igdomain.Event) error {
	switch ev.Kind {
	case igdomain.EventInboundMessage:
		return uc.handleInboundMessage(ctx, account, ev)
	case igdomain.EventEchoMessage:
		return uc.handleEcho(ctx, account, ev)
	case igdomain.EventDeletedMessage:
		return uc.handleDeleted(ctx, account, ev)
	case igdomain.EventEditedMessage:
		return uc.handleEdited(ctx, account, ev)
	case igdomain.EventReaction:
		return uc.handleReaction(ctx, account, ev)
	case igdomain.EventRead:
		return uc.handleRead(ctx, account, ev)
	case igdomain.EventPostback:
		return uc.handlePostback(ctx, account, ev)
	case igdomain.EventReferral:
		return uc.handleReferral(ctx, account, ev)
	case igdomain.EventComment, igdomain.EventLiveComment:
		return uc.handleComment(ctx, account, ev)
	case igdomain.EventStandby:
		log.Printf("[instagram] standby event account=%s (thread owned by another app)", account.IGUserID)
		return nil
	case igdomain.EventUnknown:
		log.Printf("[instagram] unhandled webhook field=%q account=%s raw=%s",
			ev.RawField, account.IGUserID, truncateRaw(ev.RawValue, 512))
		return nil
	}
	return nil
}

func (uc *HandleWebhookUseCase) handleInboundMessage(ctx context.Context, account *igdomain.Account, ev *igdomain.Event) error {
	msg := ev.Message
	if msg == nil {
		return nil
	}

	contact, conv, err := uc.resolveConversation(ctx, account, ev.ContactIGSID)
	if err != nil {
		return err
	}
	if contact.Blocked {
		log.Printf("[instagram] dropping message from blocked contact %s", contact.IGSID)
		return nil
	}
	log.Printf("[instagram] inbound mid=%s account=@%s contact=%s conversation=%s",
		msg.MID, account.Username, contact.IGSID, conv.ID)

	if err := uc.conversations.RecordInbound(ctx, conv.ID, ev.Timestamp); err != nil {
		return err
	}

	uc.ensureAssignment(conv, account)

	msgType, metadata := classifyInbound(msg)
	text := strings.TrimSpace(msg.Text)

	stored := uc.storeAttachments(ctx, conv, msg)

	senderName, senderAvatar := contact.DisplayName(), contact.ProfilePictureURL

	if text == "" && len(stored) == 0 {
		if msgType == conversation.MessageTypeUserMessage {
			msgType = conversation.MessageTypeUnsupported
		}
		return uc.record(ctx, conv, conversation.MessageDirectionInbound, historyInput{
			MessageType:       msgType,
			ProviderMessageID: msg.MID,
			From:              contact.IGSID,
			To:                account.IGUserID,
			Text:              unsupportedPlaceholder(msg),
			Timestamp:         ev.Timestamp,
			Metadata:          metadata,
			SenderName:        senderName,
			SenderAvatar:      senderAvatar,
		})
	}

	if text != "" || len(stored) == 0 {
		if err := uc.record(ctx, conv, conversation.MessageDirectionInbound, historyInput{
			MessageType:       msgType,
			ProviderMessageID: msg.MID,
			From:              contact.IGSID,
			To:                account.IGUserID,
			Text:              text,
			Timestamp:         ev.Timestamp,
			Metadata:          metadata,
			SenderName:        senderName,
			SenderAvatar:      senderAvatar,
		}); err != nil {
			return err
		}
	}

	for i, item := range stored {
		providerID := msg.MID
		if len(stored) > 1 || text != "" {
			providerID = fmt.Sprintf("%s:att%d", msg.MID, i)
		}
		if err := uc.record(ctx, conv, conversation.MessageDirectionInbound, historyInput{
			MessageType:       mediaMessageType(msgType, item.mediaType),
			ProviderMessageID: providerID,
			From:              contact.IGSID,
			To:                account.IGUserID,
			Timestamp:         ev.Timestamp,
			MediaID:           item.mediaID,
			MediaType:         item.mediaType,
			MediaURL:          item.url,
			Metadata:          metadata,
			SenderName:        senderName,
			SenderAvatar:      senderAvatar,
		}); err != nil {
			return err
		}
	}

	uc.enrichContact(ctx, account, contact)
	uc.fireWorkflowTriggers(ctx, account, conv, contact.IGSID, text, quickReplySelection(msg))
	uc.maybeReplyWithAgent(ctx, account, contact, conv, text)
	uc.scheduleAnalysis(account, conv)
	return nil
}

func (uc *HandleWebhookUseCase) scheduleAnalysis(account *igdomain.Account, conv *igdomain.Conversation) {
	if uc.analysis == nil || !(account.EnableAnalysis || account.EnableAutoStaging || account.EnableAutoMemory) {
		return
	}
	if conv.AutomationEnabled != nil && !*conv.AutomationEnabled {
		return
	}
	uc.analysis.ScheduleAnalysis(conv.ID, shared.EntryTypeInstagram)
}

func (uc *HandleWebhookUseCase) fireWorkflowTriggers(
	ctx context.Context,
	account *igdomain.Account,
	conv *igdomain.Conversation,
	contactRef string,
	text string,
	sel *workflow.OptionSelection,
) {
	if uc.workflows == nil || !account.EnableWorkflow {
		return
	}
	if conv.AutomationEnabled != nil && !*conv.AutomationEnabled {
		log.Printf("[instagram] automation disabled for conversation=%s, skipping workflow triggers", conv.ID)
		return
	}

	data := map[string]interface{}{
		"message":      text,
		"channel":      string(shared.EntryTypeInstagram),
		"workspace_id": account.WorkspaceID,
	}
	if account.WorkflowID != nil {
		data["account_workflow_id"] = *account.WorkflowID
	}
	workflow.ApplySelection(data, sel)
	workflow.ApplyContactNumber(data, contactRef)

	uc.workflows.Evaluate(workflow.TriggerEvent{
		WorkspaceID: account.WorkspaceID,
		EntryID:     conv.ID,
		EntryType:   string(shared.EntryTypeInstagram),
		TriggerType: workflow.TriggerMessageReceived,
		Data:        data,
	})

	if uc.isFirstInboundMessage(ctx, conv) {
		uc.workflows.Evaluate(workflow.TriggerEvent{
			WorkspaceID: account.WorkspaceID,
			EntryID:     conv.ID,
			EntryType:   string(shared.EntryTypeInstagram),
			TriggerType: workflow.TriggerFirstMessage,
			Data:        data,
		})
	}
}

func (uc *HandleWebhookUseCase) isFirstInboundMessage(ctx context.Context, conv *igdomain.Conversation) bool {
	if uc.messages == nil {
		return false
	}
	count, err := uc.messages.CountInboundByEntry(conv.ID, shared.EntryTypeInstagram)
	if err != nil {
		log.Printf("[instagram] could not count inbound messages for %s: %v", conv.ID, err)
		return false
	}
	return count == 1
}

func (uc *HandleWebhookUseCase) maybeReplyWithAgent(
	ctx context.Context,
	account *igdomain.Account,
	contact *igdomain.Contact,
	conv *igdomain.Conversation,
	text string,
) {
	if uc.aiReply == nil || account.AgentID == nil {
		return
	}
	var leadID *string
	if contact != nil {
		leadID = contact.LeadID
	}
	if _, err := uc.aiReply.Reply(ctx, conversation.AIReplyRequest{
		WorkspaceID:           account.WorkspaceID,
		EntryID:               conv.ID,
		EntryType:             shared.EntryTypeInstagram,
		AgentID:               *account.AgentID,
		AgentResponsesEnabled: account.EnableAgentResponses,
		AutomationEnabled:     conv.AutomationEnabled,
		Text:                  text,
		LeadID:                leadID,
	}); err != nil {
		log.Printf("[instagram] agent reply failed conversation=%s: %v", conv.ID, err)
	}
}

func (uc *HandleWebhookUseCase) handleEcho(ctx context.Context, account *igdomain.Account, ev *igdomain.Event) error {
	msg := ev.Message
	if msg == nil {
		return nil
	}
	_, conv, err := uc.resolveConversation(ctx, account, ev.ContactIGSID)
	if err != nil {
		return err
	}
	if err := uc.conversations.RecordOutbound(ctx, conv.ID, ev.Timestamp); err != nil {
		return err
	}

	_, metadata := classifyInbound(msg)
	return uc.record(ctx, conv, conversation.MessageDirectionOutbound, historyInput{
		MessageType:       conversation.MessageTypeOperator,
		ProviderMessageID: msg.MID,
		From:              account.IGUserID,
		To:                ev.ContactIGSID,
		Text:              strings.TrimSpace(msg.Text),
		Timestamp:         ev.Timestamp,
		Metadata:          metadata,
	})
}

func (uc *HandleWebhookUseCase) handleDeleted(ctx context.Context, account *igdomain.Account, ev *igdomain.Event) error {
	if ev.Message == nil || uc.messages == nil {
		return nil
	}
	existing, err := uc.messageByProviderID(ctx, account, ev, ev.Message.MID)
	if err != nil {
		if errors.Is(err, conversation.ErrMessageNotFound) {
			return nil
		}
		return err
	}
	if err := uc.messages.Delete(existing.ID); err != nil {
		return err
	}
	uc.broadcastEntryUpdate(existing.EntryID)
	return nil
}

func (uc *HandleWebhookUseCase) handleEdited(ctx context.Context, account *igdomain.Account, ev *igdomain.Event) error {
	if ev.Edit == nil || uc.messages == nil {
		return nil
	}
	existing, err := uc.messageByProviderID(ctx, account, ev, ev.Edit.MID)
	if err != nil {
		if errors.Is(err, conversation.ErrMessageNotFound) {
			return nil
		}
		return err
	}
	existing.Text = strings.TrimSpace(ev.Edit.Text)
	existing.Metadata = mergeMetadata(existing.Metadata, map[string]any{
		"instagram_edited":     true,
		"instagram_edit_count": ev.Edit.NumEdit.String(),
	})
	if err := uc.messages.Update(existing.ID, existing); err != nil {
		return err
	}
	uc.broadcastEntryUpdate(existing.EntryID)
	return nil
}

func (uc *HandleWebhookUseCase) handleReaction(ctx context.Context, account *igdomain.Account, ev *igdomain.Event) error {
	if ev.Reaction == nil || uc.messages == nil {
		return nil
	}
	existing, err := uc.messageByProviderID(ctx, account, ev, ev.Reaction.MID)
	if err != nil {
		if errors.Is(err, conversation.ErrMessageNotFound) {
			return nil
		}
		return err
	}

	payload := map[string]any{
		"instagram_reaction_action": ev.Reaction.Action,
		"instagram_reaction":        ev.Reaction.Reaction,
		"instagram_reaction_emoji":  ev.Reaction.Emoji,
		"instagram_reaction_at":     ev.Timestamp.UTC().Format(time.RFC3339),
	}
	if ev.Reaction.Action == "unreact" {
		payload["instagram_reaction"] = ""
		payload["instagram_reaction_emoji"] = ""
	}

	existing.Metadata = mergeMetadata(existing.Metadata, payload)
	if err := uc.messages.Update(existing.ID, existing); err != nil {
		return err
	}
	uc.broadcastEntryUpdate(existing.EntryID)
	return nil
}

func (uc *HandleWebhookUseCase) handleRead(ctx context.Context, account *igdomain.Account, ev *igdomain.Event) error {
	if ev.Read == nil || uc.messages == nil {
		return nil
	}
	existing, err := uc.messageByProviderID(ctx, account, ev, ev.Read.MID)
	if err != nil {
		if errors.Is(err, conversation.ErrMessageNotFound) {
			return nil
		}
		return err
	}
	if existing.DeliveryStatus == conversation.DeliveryStatusRead {
		return nil
	}
	existing.DeliveryStatus = conversation.DeliveryStatusRead
	if err := uc.messages.Update(existing.ID, existing); err != nil {
		return err
	}
	uc.broadcastEntryUpdate(existing.EntryID)
	return nil
}

func (uc *HandleWebhookUseCase) handlePostback(ctx context.Context, account *igdomain.Account, ev *igdomain.Event) error {
	if ev.Postback == nil {
		return nil
	}
	contact, conv, err := uc.resolveConversation(ctx, account, ev.ContactIGSID)
	if err != nil {
		return err
	}
	if err := uc.conversations.RecordInbound(ctx, conv.ID, ev.Timestamp); err != nil {
		return err
	}
	uc.ensureAssignment(conv, account)

	metadata, _ := json.Marshal(map[string]any{
		"instagram_postback_payload": ev.Postback.Payload,
		"instagram_postback_title":   ev.Postback.Title,
	})
	if err := uc.record(ctx, conv, conversation.MessageDirectionInbound, historyInput{
		MessageType:       conversation.MessageTypeUserMessage,
		ProviderMessageID: ev.Postback.MID,
		From:              contact.IGSID,
		To:                account.IGUserID,
		Text:              ev.Postback.Title,
		Timestamp:         ev.Timestamp,
		Metadata:          metadata,
		SenderName:        contact.DisplayName(),
		SenderAvatar:      contact.ProfilePictureURL,
	}); err != nil {
		return err
	}

	uc.fireWorkflowTriggers(ctx, account, conv, contact.IGSID, ev.Postback.Title, &workflow.OptionSelection{
		ID:    ev.Postback.Payload,
		Title: ev.Postback.Title,
		Kind:  "postback",
	})
	return nil
}

func quickReplySelection(msg *igdomain.Message) *workflow.OptionSelection {
	if msg == nil || msg.QuickReply == nil || msg.QuickReply.Payload == "" {
		return nil
	}
	return &workflow.OptionSelection{
		ID:    msg.QuickReply.Payload,
		Title: msg.Text,
		Kind:  "quick_reply",
	}
}

func (uc *HandleWebhookUseCase) handleReferral(ctx context.Context, account *igdomain.Account, ev *igdomain.Event) error {
	if ev.Referral == nil {
		return nil
	}
	_, conv, err := uc.resolveConversation(ctx, account, ev.ContactIGSID)
	if err != nil {
		return err
	}
	log.Printf("[instagram] referral account=%s conversation=%s ref=%s source=%s ad=%s",
		account.IGUserID, conv.ID, ev.Referral.Ref, ev.Referral.Source, ev.Referral.AdID)
	return nil
}

func (uc *HandleWebhookUseCase) handleComment(ctx context.Context, account *igdomain.Account, ev *igdomain.Event) error {
	cv := ev.Comment
	if cv == nil || uc.comments == nil {
		return nil
	}
	commentID := cv.ResolvedCommentID()
	if commentID == "" {
		return nil
	}

	record := &igdomain.Comment{
		WorkspaceID: account.WorkspaceID,
		IGAccountID: account.ID,
		IGCommentID: commentID,
		Text:        cv.Text,
		Timestamp:   &ev.Timestamp,
	}
	if cv.From != nil {
		record.FromIGSID = cv.From.ID
		record.FromUsername = cv.From.Username
	}
	if cv.Media != nil {
		record.IGMediaID = cv.Media.ID
	}
	if cv.ParentID != "" {
		parent := cv.ParentID
		record.ParentIGCommentID = &parent
	}
	record.IsOurs = record.FromIGSID != "" && record.FromIGSID == account.IGUserID

	if err := uc.comments.Upsert(ctx, record); err != nil {
		return err
	}

	if uc.commentRules != nil {
		uc.commentRules.Execute(ctx, record)
	}
	if uc.audience != nil {
		uc.audience.Enqueue(ctx, record)
	}
	return nil
}

func (uc *HandleWebhookUseCase) messageByProviderID(
	ctx context.Context,
	account *igdomain.Account,
	ev *igdomain.Event,
	mid string,
) (*conversation.Message, error) {
	if conv, err := uc.findConversation(ctx, account, ev.ContactIGSID); err == nil && conv != nil {
		return uc.messages.GetByEntryAndExternalMessageID(shared.EntryTypeInstagram, conv.ID, mid)
	}
	return uc.messages.GetByExternalMessageID(shared.EntryTypeInstagram, mid)
}

func (uc *HandleWebhookUseCase) findConversation(ctx context.Context, account *igdomain.Account, igsid string) (*igdomain.Conversation, error) {
	if strings.TrimSpace(igsid) == "" {
		return nil, igdomain.ErrConversationNotFound
	}
	contact, err := uc.contacts.FindByIGSID(ctx, account.ID, igsid)
	if err != nil {
		return nil, err
	}
	return uc.conversations.FindByContact(ctx, account.ID, contact.ID)
}

func (uc *HandleWebhookUseCase) resolveConversation(ctx context.Context, account *igdomain.Account, igsid string) (*igdomain.Contact, *igdomain.Conversation, error) {
	if igsid == "" {
		return nil, nil, fmt.Errorf("instagram: event has no contact id")
	}
	contact, err := uc.contacts.FindOrCreate(ctx, account.WorkspaceID, account.ID, igsid)
	if err != nil {
		return nil, nil, err
	}
	conv, err := uc.conversations.FindOrCreate(ctx, account.WorkspaceID, account.ID, contact.ID)
	if err != nil {
		return nil, nil, err
	}
	return contact, conv, nil
}

func (uc *HandleWebhookUseCase) ensureAssignment(conv *igdomain.Conversation, account *igdomain.Account) {
	if uc.assignments == nil {
		return
	}
	uc.assignments.EnsureAssignment(conv.ID, string(shared.EntryTypeInstagram), account.ID)
}

type historyInput struct {
	MessageType       conversation.MessageType
	ProviderMessageID string
	From              string
	To                string
	Text              string
	Timestamp         time.Time
	MediaID           string
	MediaType         conversation.MediaType
	MediaURL          string
	Metadata          json.RawMessage

	SenderName   string
	SenderAvatar string
}

func (uc *HandleWebhookUseCase) record(ctx context.Context, conv *igdomain.Conversation, direction conversation.MessageHistoryDirection, in historyInput) error {
	if uc.history == nil {
		return nil
	}
	return uc.history.Record(ctx, direction, conversation.MessageHistoryRecord{
		EntryID:           conv.ID,
		EntryType:         shared.EntryTypeInstagram,
		Channel:           conversation.MessageChannelInstagram,
		MessageType:       in.MessageType,
		ProviderMessageID: in.ProviderMessageID,
		From:              in.From,
		To:                in.To,
		Text:              in.Text,
		Timestamp:         in.Timestamp,
		MediaID:           in.MediaID,
		MediaType:         in.MediaType,
		MediaURL:          in.MediaURL,
		Metadata:          in.Metadata,
		SenderName:        in.SenderName,
		SenderAvatar:      in.SenderAvatar,
	})
}

func (uc *HandleWebhookUseCase) broadcastEntryUpdate(entryID string) {
	if uc.broadcaster == nil || entryID == "" {
		return
	}
	uc.broadcaster.BroadcastEntryUpdate(entryID, string(shared.EntryTypeInstagram), nil)
}

type storedAttachment struct {
	mediaID   string
	mediaType conversation.MediaType
	url       string
}

func (uc *HandleWebhookUseCase) storeAttachments(ctx context.Context, conv *igdomain.Conversation, msg *igdomain.Message) []storedAttachment {
	if uc.fileStorage == nil || uc.mediaFetcher == nil || len(msg.Attachments) == 0 {
		return nil
	}

	out := make([]storedAttachment, 0, len(msg.Attachments))
	for _, att := range msg.Attachments {
		if att == nil {
			continue
		}
		if att.Payload == nil || att.Payload.URL == "" {
			continue
		}
		kind := igdomain.MediaKindForAttachment(att.Type)
		if kind == "" {
			continue
		}

		data, contentType, err := uc.mediaFetcher.FetchMediaBytes(ctx, att.Payload.URL)
		if err != nil {
			log.Printf("[instagram] attachment download failed type=%s: %v", att.Type, err)
			continue
		}

		mediaID := uuid.NewString()
		key := fmt.Sprintf("conversations/%s/%s/%s%s",
			shared.EntryTypeInstagram, conv.ID, mediaID, extensionFor(contentType, att.Payload.URL))

		if err := uc.fileStorage.UploadFile(key, data, contentType); err != nil {
			log.Printf("[instagram] attachment upload failed key=%s: %v", key, err)
			continue
		}
		url := uc.fileStorage.GetFileURL(key)

		mediaType := conversationMediaType(kind)
		if uc.convMedia != nil {
			record := &conversation.ConversationMedia{
				ID:        mediaID,
				EntryID:   conv.ID,
				EntryType: shared.EntryTypeInstagram,
				Type:      mediaType,
				MimeType:  contentType,
				URL:       url,
				SizeBytes: int64(len(data)),
			}
			record.Normalize()
			if err := record.Validate(); err == nil {
				if err := uc.convMedia.Create(record); err != nil {
					log.Printf("[instagram] conversation media insert failed id=%s: %v", mediaID, err)
				}
			}
		}

		out = append(out, storedAttachment{mediaID: mediaID, mediaType: mediaType, url: url})
	}
	return out
}

func (uc *HandleWebhookUseCase) enrichContact(ctx context.Context, account *igdomain.Account, contact *igdomain.Contact) {
	if uc.messaging == nil || !contact.ProfileIsStale(time.Now().UTC(), profileTTL) {
		return
	}
	profile, err := uc.messaging.GetContactProfile(ctx, account.AccessToken, contact.IGSID)
	if err != nil {
		log.Printf("[instagram] contact profile fetch failed igsid=%s: %v", contact.IGSID, err)
		return
	}
	if err := uc.contacts.UpdateProfile(ctx, contact.ID, igdomain.ContactProfile{
		Username:             profile.Username,
		Name:                 profile.Name,
		ProfilePictureURL:    profile.ProfilePictureURL,
		IsVerifiedUser:       profile.IsVerifiedUser,
		FollowerCount:        profile.FollowerCount,
		IsUserFollowBusiness: profile.IsUserFollowBusiness,
		IsBusinessFollowUser: profile.IsBusinessFollowUser,
		FetchedAt:            time.Now().UTC(),
	}); err != nil {
		log.Printf("[instagram] contact profile update failed igsid=%s: %v", contact.IGSID, err)
	}
}

func classifyInbound(msg *igdomain.Message) (conversation.MessageType, json.RawMessage) {
	meta := map[string]any{}
	msgType := conversation.MessageTypeUserMessage

	if msg.ReplyTo != nil {
		switch {
		case msg.ReplyTo.Story != nil:
			msgType = conversation.MessageTypeStoryReply
			meta["instagram_story_id"] = msg.ReplyTo.Story.ID
			meta["instagram_story_url"] = msg.ReplyTo.Story.URL
			if msg.ReplyTo.Story.LinkStickerURL != "" {
				meta["instagram_story_link_sticker_url"] = msg.ReplyTo.Story.LinkStickerURL
			}
		case msg.ReplyTo.MID != "":
			meta["instagram_reply_to_mid"] = msg.ReplyTo.MID
		}
	}

	for _, att := range msg.Attachments {
		if att == nil {
			continue
		}
		switch att.Type {
		case "story_mention":
			msgType = conversation.MessageTypeStoryMention
			if att.Payload != nil {
				meta["instagram_story_mention_url"] = att.Payload.URL
			}
		case "share", "post", "ig_post":
			msgType = conversation.MessageTypePostShare
			if att.Payload != nil && att.Payload.ID != "" {
				meta["instagram_shared_post_id"] = att.Payload.ID
			}
		case "ephemeral":
			meta["instagram_ephemeral"] = true
		}
	}

	if msg.QuickReply != nil && msg.QuickReply.Payload != "" {
		meta["instagram_quick_reply_payload"] = msg.QuickReply.Payload
	}
	if msg.IsUnsupported != nil && *msg.IsUnsupported {
		msgType = conversation.MessageTypeUnsupported
		meta["instagram_unsupported"] = true
	}
	if msg.MID != "" {
		meta["instagram_mid"] = msg.MID
	}

	raw, err := json.Marshal(meta)
	if err != nil {
		return msgType, nil
	}
	return msgType, raw
}

func mediaMessageType(base conversation.MessageType, mediaType conversation.MediaType) conversation.MessageType {
	switch base {
	case conversation.MessageTypeStoryReply, conversation.MessageTypeStoryMention,
		conversation.MessageTypePostShare, conversation.MessageTypeUnsupported:
		return base
	}
	if mediaType == conversation.MediaTypeAudio {
		return conversation.MessageTypeAudio
	}
	return conversation.MessageTypeMedia
}

func conversationMediaType(kind string) conversation.MediaType {
	switch kind {
	case "image":
		return conversation.MediaTypeImage
	case "video":
		return conversation.MediaTypeVideo
	case "audio":
		return conversation.MediaTypeAudio
	default:
		return conversation.MediaTypeDocument
	}
}

func unsupportedPlaceholder(msg *igdomain.Message) string {
	for _, att := range msg.Attachments {
		if att != nil && att.Type == "ephemeral" {
			return "[disappearing media]"
		}
	}
	return "[unsupported message]"
}

func extensionFor(contentType, rawURL string) string {
	if contentType != "" {
		if exts, err := mime.ExtensionsByType(contentType); err == nil && len(exts) > 0 {
			return exts[0]
		}
	}
	if ext := path.Ext(strings.SplitN(rawURL, "?", 2)[0]); ext != "" {
		return ext
	}
	return ""
}

func mergeMetadata(existing json.RawMessage, updates map[string]any) json.RawMessage {
	merged := map[string]any{}
	if len(existing) > 0 {
		_ = json.Unmarshal(existing, &merged)
	}
	for k, v := range updates {
		merged[k] = v
	}
	raw, err := json.Marshal(merged)
	if err != nil {
		return existing
	}
	return raw
}

func truncateRaw(raw json.RawMessage, n int) string {
	if len(raw) <= n {
		return string(raw)
	}
	return string(raw[:n]) + "…"
}
