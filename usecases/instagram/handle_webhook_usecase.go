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

// profileTTL is how long a cached contact profile is considered fresh.
//
// Graph reads cost against a budget that scales with the account's audience
// activity (4800 x impressions per 24h), so a brand-new tenant has almost no
// budget. Enrichment is therefore lazy and cached rather than per-message.
const profileTTL = 7 * 24 * time.Hour

// AssignmentService is the subset of the round-robin assignment service we need.
// Declared here as a narrow port so the Instagram usecase does not depend on the
// whole conversation usecase package.
//
// The third argument is the channel account id. For WhatsApp that is the business
// phone; for Instagram it is the Instagram account, which is what keeps
// round-robin pools separate per connected account.
type AssignmentService interface {
	EnsureAssignment(entryID, entryType, accountID string) string
}

// AIReplier lets an AI agent attend this channel's conversations. Declared here
// as a narrow port for the same reason AssignmentService is: the Instagram
// usecase depends on the contract, not on the conversation usecase package.
//
// A nil message with a nil error means "deliberately not answered", automation
// off, loop suspected, empty body, or a closed provider window. That is a normal
// outcome, not a failure.
type AIReplier interface {
	Reply(ctx context.Context, req conversation.AIReplyRequest) (*conversation.Message, error)
}

// WorkflowTrigger fires workflow triggers for a conversation. Narrow port, same
// reasoning as AIReplier: the workflow engine keys on (entry_id, entry_type) and
// needs nothing Instagram-specific.
type WorkflowTrigger interface {
	Evaluate(event workflow.TriggerEvent)
}

// AnalysisScheduler stamps a conversation for deferred AI analysis.
//
// Instagram's EnableAnalysis switch existed on the account row from day one and
// did nothing, because nothing ever scheduled the job for this channel. This is
// the missing call.
type AnalysisScheduler interface {
	ScheduleAnalysis(entryID string, entryType shared.EntryType)
}

// CommentRuleEvaluator applies comment automation to one mirrored comment.
// Narrow port for the same reason as the others.
type CommentRuleEvaluator interface {
	Execute(ctx context.Context, comment *igdomain.Comment)
}

// MediaFetcher downloads an attachment from a signed CDN URL. Narrowed to one
// method so the webhook usecase does not pull in the whole posts client.
type MediaFetcher interface {
	FetchMediaBytes(ctx context.Context, url string) (data []byte, contentType string, err error)
}

// HandleWebhookUseCase turns one normalized Instagram entry into CRM state.
type HandleWebhookUseCase struct {
	accounts      igdomain.AccountRepository
	contacts      igdomain.ContactRepository
	conversations igdomain.ConversationRepository
	comments      igdomain.CommentRepository
	mediaRepo     igdomain.MediaRepository
	messaging     igdomain.MessagingService
	mediaFetcher  MediaFetcher

	history     conversation.MessageHistoryManager
	messages    conversation.MessageRepository
	convMedia   conversation.ConversationMediaRepository
	fileStorage media.FileStorage
	broadcaster conversation.EventBroadcaster
	assignments AssignmentService
	// aiReply is the channel-agnostic AI attendant. Optional: when unset the
	// channel simply has no agent, exactly as before.
	aiReply AIReplier
	// workflows fires trigger events. Optional: unset means the channel runs no
	// workflows.
	workflows WorkflowTrigger
	// commentRules applies comment automation. Optional.
	commentRules CommentRuleEvaluator
	// analysis schedules deferred AI analysis. Optional.
	analysis AnalysisScheduler
}

// HandleWebhookDeps groups the dependencies so the constructor stays readable as
// the usecase grows.
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
	}
}

// ErrUnknownAccount means the event addresses an account we do not serve. The
// consumer treats it as terminal: retrying can never make the account appear.
var ErrUnknownAccount = errors.New("instagram: webhook for an unknown account")

// Execute processes one entry envelope.
//
// Events are already sorted by their own timestamp by NormalizeEntry, because
// Meta gives no ordering guarantee and directs implementers to order by the
// webhook timestamp rather than arrival order.
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

	// Every event in an entry belongs to one account, so it is resolved once.
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
		// We do not own thread control, so the message is recorded for context
		// but no automation or reply is triggered.
		log.Printf("[instagram] standby event account=%s (thread owned by another app)", account.IGUserID)
		return nil
	case igdomain.EventUnknown:
		// Three subscribable fields have no published payload shape, so the raw
		// value is logged rather than guessed at.
		log.Printf("[instagram] unhandled webhook field=%q account=%s raw=%s",
			ev.RawField, account.IGUserID, truncateRaw(ev.RawValue, 512))
		return nil
	}
	return nil
}

// ---------------------------------------------------------------- messages

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

	// The customer clock is the anchor for the sliding 24h window, so it moves
	// before anything that could fail.
	if err := uc.conversations.RecordInbound(ctx, conv.ID, ev.Timestamp); err != nil {
		return err
	}

	uc.ensureAssignment(conv, account)

	msgType, metadata := classifyInbound(msg)
	text := strings.TrimSpace(msg.Text)

	// Attachments are an ARRAY and can hold several items in one message, so each
	// is stored and recorded separately. `ephemeral` carries no payload at all.
	stored := uc.storeAttachments(ctx, conv, msg)

	// The contact is already loaded here, so naming the sender for the live
	// broadcast is free; without it the CRM renders the raw IGSID until reload.
	senderName, senderAvatar := contact.DisplayName(), contact.ProfilePictureURL

	// A message with neither text nor storable media still needs a row so the
	// operator sees that something arrived.
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
		// Each attachment gets a distinct provider id suffix so the unique index
		// on (entry_type, external_message_id) does not reject the second one.
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
	uc.fireWorkflowTriggers(ctx, account, conv, text, quickReplySelection(msg))
	uc.maybeReplyWithAgent(ctx, account, conv, text)
	uc.scheduleAnalysis(account, conv)
	return nil
}

// scheduleAnalysis stamps the conversation for deferred AI analysis.
//
// Gated on the account's own switches, exactly as the agent and workflows are,
// so one toggle in the UI means one behaviour.
func (uc *HandleWebhookUseCase) scheduleAnalysis(account *igdomain.Account, conv *igdomain.Conversation) {
	if uc.analysis == nil || !(account.EnableAnalysis || account.EnableAutoStaging) {
		return
	}
	if conv.AutomationEnabled != nil && !*conv.AutomationEnabled {
		return
	}
	uc.analysis.ScheduleAnalysis(conv.ID, shared.EntryTypeInstagram)
}

// fireWorkflowTriggers starts or advances workflows for this conversation.
//
// Gating matches the AI agent exactly, the account's workflow switch, overridden
// per conversation by the automation toggle, so pausing automation on a
// conversation silences BOTH the agent and its workflows. Otherwise an operator
// who took over would still be interrupted by a workflow step.
//
// The trigger event itself is channel-neutral, so every node that keys on
// (entry_id, entry_type) works here unchanged.
// fireWorkflowTriggers evaluates workflow triggers for one inbound event.
//
// sel is non-nil only when the contact TAPPED a quick reply or a postback
// button rather than typing. Without it a tap cannot reach the option's own
// branch, AdvanceOnReply routes on the option id and falls back to no_match.
func (uc *HandleWebhookUseCase) fireWorkflowTriggers(
	ctx context.Context,
	account *igdomain.Account,
	conv *igdomain.Conversation,
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

	uc.workflows.Evaluate(workflow.TriggerEvent{
		WorkspaceID: account.WorkspaceID,
		EntryID:     conv.ID,
		EntryType:   string(shared.EntryTypeInstagram),
		TriggerType: workflow.TriggerMessageReceived,
		Data:        data,
	})

	// trigger_first_message fires on the contact's first inbound message. The
	// conversation's customer clock is set by RecordInbound before this runs, so
	// "first" is derived from whether one had been recorded previously.
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

// isFirstInboundMessage reports whether the message just recorded is the first
// one this contact has sent.
//
// It counts ALL of the contact's messages, not a page of recent history. A
// windowed count is wrong in a way that only surfaces deep in a conversation:
// once the agent has answered with several messages, the most recent rows are
// mostly outbound, exactly one is inbound, and the check reports a "first
// message" long after the first, starting a duplicate workflow run and
// greeting the contact again.
func (uc *HandleWebhookUseCase) isFirstInboundMessage(ctx context.Context, conv *igdomain.Conversation) bool {
	if uc.messages == nil {
		return false
	}
	// The message is already persisted, so exactly one means this is it.
	count, err := uc.messages.CountInboundByEntry(conv.ID, shared.EntryTypeInstagram)
	if err != nil {
		// Fail closed: a spurious trigger is worse than a missed one here.
		log.Printf("[instagram] could not count inbound messages for %s: %v", conv.ID, err)
		return false
	}
	return count == 1
}

// maybeReplyWithAgent hands the message to the channel-agnostic AI service.
//
// The service owns every decision, automation gating, loop protection, the
// outbound window, so this stays a hand-off rather than a second place where
// "should the bot answer?" is implemented.
//
// Failures are logged, never returned: an AI problem must not fail the webhook
// and trigger a redelivery of a message that was already stored.
func (uc *HandleWebhookUseCase) maybeReplyWithAgent(
	ctx context.Context,
	account *igdomain.Account,
	conv *igdomain.Conversation,
	text string,
) {
	if uc.aiReply == nil || account.AgentID == nil {
		return
	}
	if _, err := uc.aiReply.Reply(ctx, conversation.AIReplyRequest{
		WorkspaceID:           account.WorkspaceID,
		EntryID:               conv.ID,
		EntryType:             shared.EntryTypeInstagram,
		AgentID:               *account.AgentID,
		AgentResponsesEnabled: account.EnableAgentResponses,
		AutomationEnabled:     conv.AutomationEnabled,
		Text:                  text,
	}); err != nil {
		log.Printf("[instagram] agent reply failed conversation=%s: %v", conv.ID, err)
	}
}

// handleEcho reconciles a message we sent.
//
// Our own outbound messages come back to us as an echo, so without a
// read-before-insert every sent message would appear twice in the transcript.
// The history manager already dedups on the provider id, so recording the echo
// is safe and also covers messages sent from the Instagram app directly.
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

// handleDeleted tombstones an unsent message rather than inserting a new row:
// the tombstone arrives with the SAME mid as the original.
func (uc *HandleWebhookUseCase) handleDeleted(ctx context.Context, account *igdomain.Account, ev *igdomain.Event) error {
	if ev.Message == nil || uc.messages == nil {
		return nil
	}
	existing, err := uc.messages.GetByExternalMessageID(shared.EntryTypeInstagram, ev.Message.MID)
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

// handleEdited replaces the stored text for an edited DM.
func (uc *HandleWebhookUseCase) handleEdited(ctx context.Context, account *igdomain.Account, ev *igdomain.Event) error {
	if ev.Edit == nil || uc.messages == nil {
		return nil
	}
	existing, err := uc.messages.GetByExternalMessageID(shared.EntryTypeInstagram, ev.Edit.MID)
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

// handleReaction records a reaction against the target message.
//
// Instagram never echoes our own reactions, so only the contact's reactions
// arrive here; ours are recorded locally at send time.
func (uc *HandleWebhookUseCase) handleReaction(ctx context.Context, account *igdomain.Account, ev *igdomain.Event) error {
	if ev.Reaction == nil || uc.messages == nil {
		return nil
	}
	existing, err := uc.messages.GetByExternalMessageID(shared.EntryTypeInstagram, ev.Reaction.MID)
	if err != nil {
		if errors.Is(err, conversation.ErrMessageNotFound) {
			return nil
		}
		return err
	}

	// The reaction value is not a closed enum, the docs list different sets and
	// allow "other", so the raw string and emoji are both stored verbatim.
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

// handleRead marks our outbound message as read.
//
// Instagram sends a specific message id, NOT a watermark, so only that message
// can be marked, "everything before T" cannot be inferred.
func (uc *HandleWebhookUseCase) handleRead(ctx context.Context, account *igdomain.Account, ev *igdomain.Event) error {
	if ev.Read == nil || uc.messages == nil {
		return nil
	}
	existing, err := uc.messages.GetByExternalMessageID(shared.EntryTypeInstagram, ev.Read.MID)
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

// handlePostback records an icebreaker or CTA tap.
//
// The CRM keys off postback.payload, not the visible title, because the title is
// display text that can change.
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

	// A postback tap fired no workflow trigger at all before this: the run
	// stayed parked at the prompt until it timed out, even though the contact
	// had answered. The payload is the option id, exactly as for quick replies.
	uc.fireWorkflowTriggers(ctx, account, conv, ev.Postback.Title, &workflow.OptionSelection{
		ID:    ev.Postback.Payload,
		Title: ev.Postback.Title,
		Kind:  "postback",
	})
	return nil
}

// quickReplySelection lifts the tapped quick reply out of an inbound message.
//
// Instagram reports the tap as an ordinary message whose text is the button's
// TITLE, with the payload tucked into message.quick_reply. Branching on the
// title would break the moment an author reworded a label, so only the payload
// is treated as the option id.
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

// handleReferral stores ad/ig.me attribution on the conversation.
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

// ---------------------------------------------------------------- comments

// handleComment mirrors a comment locally so the moderation queue is
// push-driven. The Graph comments edge cannot be filtered by timestamp, so
// webhooks are the only reliable incremental source.
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
	// A comment authored by our own account arrives with our IGSID as the sender.
	record.IsOurs = record.FromIGSID != "" && record.FromIGSID == account.IGUserID

	if err := uc.comments.Upsert(ctx, record); err != nil {
		return err
	}

	// Automation runs AFTER the mirror is written, so a rule's own public reply
	// (which arrives back as another webhook) is evaluated against stored state
	// rather than racing it.
	if uc.commentRules != nil {
		uc.commentRules.Execute(ctx, record)
	}
	return nil
}

// ---------------------------------------------------------------- helpers

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

// ensureAssignment hands the conversation to an operator. Assignment already
// keys on (entry_id, entry_type), so it works for Instagram unchanged.
func (uc *HandleWebhookUseCase) ensureAssignment(conv *igdomain.Conversation, account *igdomain.Account) {
	if uc.assignments == nil {
		return
	}
	// The account id takes the business-phone slot so each connected Instagram
	// account keeps its own round-robin pool.
	uc.assignments.EnsureAssignment(conv.ID, string(shared.EntryTypeInstagram), account.ID)
}

// historyInput is the per-message payload for the shared history manager.
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

	// SenderName/SenderAvatar label the live broadcast, filled from the contact
	// the inbound path already loaded so the hub does not re-read it.
	SenderName   string
	SenderAvatar string
}

// record persists and broadcasts through the SHARED history manager, so
// Instagram reuses the same dedup, persistence and websocket fan-out as every
// other channel instead of reimplementing them.
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

// storedAttachment is a downloaded attachment persisted to object storage.
type storedAttachment struct {
	mediaID   string
	mediaType conversation.MediaType
	url       string
}

// storeAttachments downloads each attachment and persists it.
//
// The attachment URL is a short-lived signed CDN link, so it must be fetched
// during processing and re-hosted; keeping the URL would leave a dead link.
func (uc *HandleWebhookUseCase) storeAttachments(ctx context.Context, conv *igdomain.Conversation, msg *igdomain.Message) []storedAttachment {
	if uc.fileStorage == nil || uc.mediaFetcher == nil || len(msg.Attachments) == 0 {
		return nil
	}

	out := make([]storedAttachment, 0, len(msg.Attachments))
	for _, att := range msg.Attachments {
		if att == nil {
			continue
		}
		// `ephemeral` (disappearing media) carries no payload at all, a naive
		// dereference here would panic.
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

		if err := uc.fileStorage.UploadFile(key, data); err != nil {
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

// enrichContact refreshes a stale contact profile.
func (uc *HandleWebhookUseCase) enrichContact(ctx context.Context, account *igdomain.Account, contact *igdomain.Contact) {
	if uc.messaging == nil || !contact.ProfileIsStale(time.Now().UTC(), profileTTL) {
		return
	}
	profile, err := uc.messaging.GetContactProfile(ctx, account.AccessToken, contact.IGSID)
	if err != nil {
		// Profile enrichment is cosmetic; a failure must never drop a message.
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

// classifyInbound derives the CRM message type and the metadata to attach.
//
// reply_to is a UNION: a story object for a story reply, a mid for an inline
// reply. Story mentions instead arrive as an attachment type.
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
			// Disappearing media has no URL and cannot be stored.
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

// mediaMessageType keeps a story reply/mention classification when the message
// also carries media, so the UI can still render the story context.
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

// unsupportedPlaceholder gives the operator something to see for a message we
// cannot render. Note that gifs and stickers produce NO webhook at all, so they
// never reach this path.
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
