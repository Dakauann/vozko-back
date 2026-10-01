package instagram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"vozko/domain/conversation"
	igdomain "vozko/domain/instagram"
	mm "vozko/domain/metamessaging"
	"vozko/domain/shared"
	"vozko/domain/workflow"
	conversation_usecase "vozko/usecases/conversation"
	"vozko/usecases/metachannel"
)

const profileTTL = 7 * 24 * time.Hour

const metadataPrefix = "instagram"

type AssignmentService interface {
	EnsureAssignment(entryID, entryType, accountID string) string
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

	transcript   *metachannel.Transcript
	assignments  AssignmentService
	automation   *conversation_usecase.InboundAutomation
	commentRules CommentRuleEvaluator
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
	Attachments  conversation.MediaStore
	AdOrigins    conversation.AdOriginRecorder
	Broadcaster  conversation.EventBroadcaster
	Assignments  AssignmentService
	AIReply      conversation_usecase.AgentReplier
	Workflows    conversation_usecase.WorkflowEvaluator
	CommentRules CommentRuleEvaluator
	Analysis     conversation_usecase.AnalysisRequester
	Audience     AudienceEnqueuer
}

func NewHandleWebhookUseCase(d HandleWebhookDeps) *HandleWebhookUseCase {
	transcript := &metachannel.Transcript{
		EntryType: shared.EntryTypeInstagram,
		Channel:   conversation.MessageChannelInstagram,
		Prefix:    metadataPrefix,
		History:   d.History,
		Media:     d.Attachments,
		Ads:       d.AdOrigins,
	}
	if d.Messages != nil {
		transcript.Messages = d.Messages
	}
	if d.Broadcaster != nil {
		transcript.Broadcaster = d.Broadcaster
	}
	if d.MediaFetcher != nil {
		transcript.Fetch = d.MediaFetcher.FetchMediaBytes
	}
	return &HandleWebhookUseCase{
		accounts:      d.Accounts,
		contacts:      d.Contacts,
		conversations: d.Conversations,
		comments:      d.Comments,
		mediaRepo:     d.Media,
		messaging:     d.Messaging,
		transcript:    transcript,
		assignments:   d.Assignments,
		automation:    conversation_usecase.NewInboundAutomation(d.Workflows, d.AIReply, d.Analysis, d.Messages),
		commentRules:  d.CommentRules,
		audience:      d.Audience,
	}
}

var ErrUnknownAccount = errors.New("instagram: webhook for an unknown account")

func (uc *HandleWebhookUseCase) Execute(ctx context.Context, env *mm.EntryEnvelope) error {
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
		if ev.AccountExternalID == "" {
			continue
		}
		account, err := uc.accounts.FindByIGUserID(ctx, ev.AccountExternalID)
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
	case mm.EventInboundMessage:
		return uc.handleInboundMessage(ctx, account, ev)
	case mm.EventEchoMessage:
		return uc.handleEcho(ctx, account, ev)
	case mm.EventDeletedMessage:
		if ev.Message == nil {
			return nil
		}
		return uc.transcript.Tombstone(uc.entryFor(ctx, account, ev), ev.Message.MID)
	case mm.EventEditedMessage:
		if ev.Edit == nil {
			return nil
		}
		return uc.transcript.ApplyEdit(uc.entryFor(ctx, account, ev), ev.Edit)
	case mm.EventReaction:
		if ev.Reaction == nil {
			return nil
		}
		return uc.transcript.ApplyReaction(uc.entryFor(ctx, account, ev), ev.Reaction, ev.Timestamp)
	case mm.EventRead:
		if ev.Read == nil {
			return nil
		}
		return uc.transcript.MarkRead(uc.entryFor(ctx, account, ev), ev.Read.MID)
	case mm.EventPostback:
		return uc.handlePostback(ctx, account, ev)
	case mm.EventReferral:
		return uc.handleReferral(ctx, account, ev)
	case igdomain.EventComment, igdomain.EventLiveComment:
		return uc.handleComment(ctx, account, ev)
	case mm.EventStandby:
		log.Printf("[instagram] standby event account=%s (thread owned by another app)", account.IGUserID)
		return nil
	case mm.EventUnknown:
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

	contact, conv, err := uc.resolveConversation(ctx, account, ev.ContactExternalID)
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

	text, err := uc.transcript.RecordMessage(ctx, conv.ID, conversation.SentByContact(contact.IGSID), msg, ev.Timestamp, contactParty(account, contact))
	if err != nil {
		return err
	}

	uc.enrichContact(ctx, account, contact, msg.MID)
	uc.automation.Dispatch(ctx, automationInput(account, contact, conv, text, metachannel.QuickReplySelection(msg)))
	return nil
}

func (uc *HandleWebhookUseCase) handleEcho(ctx context.Context, account *igdomain.Account, ev *igdomain.Event) error {
	msg := ev.Message
	if msg == nil {
		return nil
	}
	_, conv, err := uc.resolveConversation(ctx, account, ev.ContactExternalID)
	if err != nil {
		return err
	}
	if err := uc.conversations.RecordOutbound(ctx, conv.ID, ev.Timestamp); err != nil {
		return err
	}

	_, metadata := metachannel.Classify(msg, metadataPrefix)
	return uc.transcript.Record(ctx, conv.ID, conversation.SentExternally(),
		metachannel.Party{From: account.IGUserID, To: ev.ContactExternalID},
		metachannel.HistoryInput{
			MessageType:       conversation.MessageTypeOperator,
			ProviderMessageID: msg.MID,
			Text:              strings.TrimSpace(msg.Text),
			Timestamp:         ev.Timestamp,
			Metadata:          metadata,
		})
}

func (uc *HandleWebhookUseCase) handlePostback(ctx context.Context, account *igdomain.Account, ev *igdomain.Event) error {
	if ev.Postback == nil {
		return nil
	}
	contact, conv, err := uc.resolveConversation(ctx, account, ev.ContactExternalID)
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
	if err := uc.transcript.Record(ctx, conv.ID, conversation.SentByContact(contact.IGSID), contactParty(account, contact),
		metachannel.HistoryInput{
			MessageType:       conversation.MessageTypeUserMessage,
			ProviderMessageID: ev.Postback.MID,
			Text:              ev.Postback.Title,
			Timestamp:         ev.Timestamp,
			Metadata:          metadata,
		}); err != nil {
		return err
	}

	uc.automation.FireWorkflows(ctx, automationInput(account, contact, conv, ev.Postback.Title, &workflow.OptionSelection{
		ID:    ev.Postback.Payload,
		Title: ev.Postback.Title,
		Kind:  "postback",
	}))
	return nil
}

func (uc *HandleWebhookUseCase) handleReferral(ctx context.Context, account *igdomain.Account, ev *igdomain.Event) error {
	if ev.Referral == nil {
		return nil
	}
	_, conv, err := uc.resolveConversation(ctx, account, ev.ContactExternalID)
	if err != nil {
		return err
	}
	uc.transcript.RecordReferral(ctx, conv.ID, ev.Referral)
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
	return uc.transcript.Find(uc.entryFor(ctx, account, ev), mid)
}

func (uc *HandleWebhookUseCase) entryFor(ctx context.Context, account *igdomain.Account, ev *igdomain.Event) string {
	conv, err := uc.findConversation(ctx, account, ev.ContactExternalID)
	if err != nil || conv == nil {
		return ""
	}
	return conv.ID
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

func contactParty(account *igdomain.Account, contact *igdomain.Contact) metachannel.Party {
	return metachannel.Party{
		From:         contact.IGSID,
		To:           account.IGUserID,
		SenderName:   contact.DisplayName(),
		SenderAvatar: contact.ProfilePictureURL,
	}
}

func (uc *HandleWebhookUseCase) enrichContact(ctx context.Context, account *igdomain.Account, contact *igdomain.Contact, mid string) {
	if uc.messaging == nil || !contact.ProfileIsStale(time.Now().UTC(), profileTTL) {
		return
	}
	profile, err := uc.messaging.GetContactProfile(ctx, account.AccessToken, contact.IGSID)
	if err != nil {
		log.Printf("[instagram] contact profile fetch failed igsid=%s: %v", contact.IGSID, err)
		uc.fillUsernameFromMessage(ctx, account, contact, mid)
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

func (uc *HandleWebhookUseCase) fillUsernameFromMessage(ctx context.Context, account *igdomain.Account, contact *igdomain.Contact, mid string) {
	if contact.Username != "" || mid == "" {
		return
	}
	sender, err := uc.messaging.GetMessageSender(ctx, account.AccessToken, mid)
	if err != nil {
		log.Printf("[instagram] message sender lookup failed igsid=%s: %v", contact.IGSID, err)
		return
	}
	if sender.ID != contact.IGSID || sender.Username == "" {
		return
	}
	if err := uc.contacts.FillUsername(ctx, contact.ID, sender.Username); err != nil {
		log.Printf("[instagram] contact username update failed igsid=%s: %v", contact.IGSID, err)
	}
}

func truncateRaw(raw json.RawMessage, n int) string {
	if len(raw) <= n {
		return string(raw)
	}
	return string(raw[:n]) + "…"
}

func automationInput(
	account *igdomain.Account,
	contact *igdomain.Contact,
	conv *igdomain.Conversation,
	text string,
	sel *workflow.OptionSelection,
) conversation_usecase.InboundAutomationInput {
	in := conversation_usecase.InboundAutomationInput{
		WorkspaceID:          account.WorkspaceID,
		EntryID:              conv.ID,
		EntryType:            shared.EntryTypeInstagram,
		Text:                 text,
		Selection:            sel,
		ConversationOverride: conv.AutomationEnabled,
		Config:               account.Automation(),
	}
	if contact != nil {
		in.ContactRef = contact.IGSID
		in.LeadID = contact.LeadID
	}
	return in
}
