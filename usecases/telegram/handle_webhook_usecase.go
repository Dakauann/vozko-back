package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"mime"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"vozko/domain/conversation"
	"vozko/domain/media"
	"vozko/domain/shared"
	tgdomain "vozko/domain/telegram"
	"vozko/domain/workflow"
)

// profileTTL is how long a cached contact profile is considered fresh.
//
// Telegram puts first_name/username/language_code straight in every update, so
// the only thing enrichment adds is the avatar — which makes a long TTL correct
// rather than merely cheap.
const profileTTL = 7 * 24 * time.Hour

// ErrUnknownAccount means the update addresses a bot we do not serve. The
// consumer treats it as terminal: retrying can never make the account appear.
var ErrUnknownAccount = errors.New("telegram: webhook for an unknown account")

// AssignmentService is the round-robin port. Narrow by design so this package
// does not depend on the whole conversation usecase package.
//
// The third argument is the channel account id, which is what keeps each bot's
// round-robin pool separate.
type AssignmentService interface {
	EnsureAssignment(entryID, entryType, accountID string) string
}

// AIReplier lets an AI agent attend this channel. A nil message with a nil error
// means "deliberately not answered" — automation off, loop suspected, empty
// body — which is a normal outcome, not a failure.
type AIReplier interface {
	Reply(ctx context.Context, req conversation.AIReplyRequest) (*conversation.Message, error)
}

// WorkflowTrigger fires workflow triggers. The event is channel-neutral, so
// every node that keys on (entry_id, entry_type) works here unchanged.
type WorkflowTrigger interface {
	Evaluate(event workflow.TriggerEvent)
}

// AnalysisScheduler stamps a conversation for deferred AI analysis.
//
// The analysis job debounces on inactivity, so this only records that the
// conversation moved; it does not run anything inline.
type AnalysisScheduler interface {
	ScheduleAnalysis(entryID string, entryType shared.EntryType)
}

// LeadLinker resolves a shared phone number to a CRM lead.
//
// Telegram never volunteers a phone number; it arrives only when the customer
// taps a request_contact button. That consent is the one moment a Telegram
// contact can be bridged to the rest of the CRM, so it is worth acting on.
type LeadLinker interface {
	FindLeadIDByPhone(ctx context.Context, workspaceID, phone string) (string, error)
}

// HandleWebhookUseCase turns one normalized Telegram update into CRM state.
type HandleWebhookUseCase struct {
	accounts      tgdomain.AccountRepository
	contacts      tgdomain.ContactRepository
	conversations tgdomain.ConversationRepository
	deepLinks     tgdomain.DeepLinkRepository
	api           tgdomain.BotAPI

	history     conversation.MessageHistoryManager
	messages    conversation.MessageRepository
	convMedia   conversation.ConversationMediaRepository
	fileStorage media.FileStorage
	broadcaster conversation.EventBroadcaster
	assignments AssignmentService
	aiReply     AIReplier
	workflows   WorkflowTrigger
	leads       LeadLinker
	analysis    AnalysisScheduler
}

// HandleWebhookDeps groups the dependencies so the constructor stays readable.
type HandleWebhookDeps struct {
	Accounts      tgdomain.AccountRepository
	Contacts      tgdomain.ContactRepository
	Conversations tgdomain.ConversationRepository
	DeepLinks     tgdomain.DeepLinkRepository
	API           tgdomain.BotAPI

	History     conversation.MessageHistoryManager
	Messages    conversation.MessageRepository
	ConvMedia   conversation.ConversationMediaRepository
	FileStorage media.FileStorage
	Broadcaster conversation.EventBroadcaster
	Assignments AssignmentService
	AIReply     AIReplier
	Workflows   WorkflowTrigger
	Leads       LeadLinker
	Analysis    AnalysisScheduler
}

func NewHandleWebhookUseCase(d HandleWebhookDeps) *HandleWebhookUseCase {
	return &HandleWebhookUseCase{
		accounts:      d.Accounts,
		contacts:      d.Contacts,
		conversations: d.Conversations,
		deepLinks:     d.DeepLinks,
		api:           d.API,
		history:       d.History,
		messages:      d.Messages,
		convMedia:     d.ConvMedia,
		fileStorage:   d.FileStorage,
		broadcaster:   d.Broadcaster,
		assignments:   d.Assignments,
		aiReply:       d.AIReply,
		workflows:     d.Workflows,
		leads:         d.Leads,
		analysis:      d.Analysis,
	}
}

// QueuedUpdate is the unit published to the queue: one raw update plus the
// tenant the ingest handler resolved it to.
//
// The account id travels alongside the payload because the update itself carries
// no bot identity — it is resolved from the URL at ingest and must not be
// re-derived later.
type QueuedUpdate struct {
	AccountID string          `json:"account_id"`
	Update    json.RawMessage `json:"update"`
}

// Execute processes one queued update.
func (uc *HandleWebhookUseCase) Execute(ctx context.Context, q *QueuedUpdate) error {
	if q == nil || len(q.Update) == 0 {
		return nil
	}

	update, err := tgdomain.DecodeUpdate(q.Update)
	if err != nil {
		return err
	}

	account, err := uc.resolveAccount(ctx, q.AccountID, update)
	if err != nil {
		return err
	}

	ev := tgdomain.NormalizeUpdate(account.ID, update, q.Update)
	if ev == nil {
		return nil
	}

	log.Printf("[telegram] update=%d account=@%s kind=%s chat=%d",
		ev.UpdateID, account.BotUsername, ev.Kind, ev.ChatID)

	return uc.handleEvent(ctx, account, ev)
}

// resolveAccount finds the tenant for an update.
//
// Bot mode resolves from the account id the ingest handler read out of the URL.
// Business mode overrides it with the connection id, because the platform bot's
// single endpoint serves every tenant and the URL says nothing about which.
func (uc *HandleWebhookUseCase) resolveAccount(ctx context.Context, accountID string, u *tgdomain.Update) (*tgdomain.Account, error) {
	if connectionID := businessConnectionIDOf(u); connectionID != "" {
		account, err := uc.accounts.FindByBusinessConnectionID(ctx, connectionID)
		if err == nil {
			return account, nil
		}
		if !errors.Is(err, tgdomain.ErrAccountNotFound) {
			return nil, err
		}
		// A business_connection update for a connection we have never seen is the
		// pairing handshake: the account row is found by the URL instead, and the
		// handler binds the connection to it.
		if u.BusinessConnection == nil {
			return nil, ErrUnknownAccount
		}
	}

	if accountID == "" {
		return nil, ErrUnknownAccount
	}
	account, err := uc.accounts.FindByIDForWebhook(ctx, accountID)
	if err != nil {
		if errors.Is(err, tgdomain.ErrAccountNotFound) {
			return nil, ErrUnknownAccount
		}
		return nil, err
	}
	return account, nil
}

func businessConnectionIDOf(u *tgdomain.Update) string {
	switch {
	case u.BusinessConnection != nil:
		return u.BusinessConnection.ID
	case u.BusinessMessage != nil:
		return u.BusinessMessage.BusinessConnectionID
	case u.EditedBusinessMessage != nil:
		return u.EditedBusinessMessage.BusinessConnectionID
	case u.DeletedBusinessMessages != nil:
		return u.DeletedBusinessMessages.BusinessConnectionID
	}
	return ""
}

func (uc *HandleWebhookUseCase) handleEvent(ctx context.Context, account *tgdomain.Account, ev *tgdomain.Event) error {
	switch ev.Kind {
	case tgdomain.EventInboundMessage:
		return uc.handleInbound(ctx, account, ev)
	case tgdomain.EventContactShared:
		return uc.handleContactShared(ctx, account, ev)
	case tgdomain.EventOutboundMessage:
		return uc.handleOutbound(ctx, account, ev)
	case tgdomain.EventEditedMessage:
		return uc.handleEdited(ctx, account, ev)
	case tgdomain.EventDeletedMessages:
		return uc.handleDeleted(ctx, account, ev)
	case tgdomain.EventCallbackQuery:
		return uc.handleCallbackQuery(ctx, account, ev)
	case tgdomain.EventBlocked, tgdomain.EventUnblocked:
		return uc.handleBlockToggle(ctx, account, ev)
	case tgdomain.EventBusinessConnection:
		return uc.handleBusinessConnection(ctx, account, ev)
	case tgdomain.EventUnknown:
		// The Bot API adds update kinds several times a year. Logging the raw
		// payload means a new one is visible the day it starts arriving, rather
		// than being silently discarded.
		log.Printf("[telegram] unhandled update kind account=@%s raw=%s",
			account.BotUsername, truncateRaw(ev.Raw, 512))
		return nil
	}
	return nil
}

// ---------------------------------------------------------------- messages

func (uc *HandleWebhookUseCase) handleInbound(ctx context.Context, account *tgdomain.Account, ev *tgdomain.Event) error {
	contact, conv, err := uc.resolveConversation(ctx, account, ev)
	if err != nil {
		return err
	}

	// A group chat is stored so the transcript exists, but never automated: with
	// privacy mode on a bot sees only commands and replies there, so an agent
	// answering from it would be answering half a conversation.
	private := conv.IsPrivate()

	if contact.Blocked {
		// An inbound message proves the contact can reach us again. Telegram also
		// sends my_chat_member on unblock, but relying on that alone leaves the
		// composer disabled if that update was ever missed.
		if err := uc.contacts.SetBlocked(ctx, contact.ID, false, ev.Timestamp); err == nil {
			contact.Blocked = false
		}
	}

	// The customer clock moves before anything that can fail: it anchors the
	// business-mode window and orders the inbox.
	if err := uc.conversations.RecordInbound(ctx, conv.ID, ev.Timestamp); err != nil {
		return err
	}
	if private {
		uc.ensureAssignment(conv, account)
	}

	// A /start payload is the channel's only attribution mechanism, so it is
	// bound before the message is recorded — a workflow triggered by this very
	// message can then already see it.
	uc.bindDeepLink(ctx, account, conv, ev)

	if err := uc.recordInboundMessage(ctx, account, contact, conv, ev); err != nil {
		return err
	}

	uc.enrichContact(ctx, account, contact)

	if private {
		uc.fireWorkflowTriggers(ctx, account, conv, ev.Text, nil)
		uc.maybeReplyWithAgent(ctx, account, conv, ev.Text)
		uc.scheduleAnalysis(account, conv)
	}
	return nil
}

// scheduleAnalysis stamps the conversation for deferred AI analysis.
//
// Gated on the account's own switch, exactly as the agent and workflows are, so
// one toggle in the UI means one behaviour.
func (uc *HandleWebhookUseCase) scheduleAnalysis(account *tgdomain.Account, conv *tgdomain.Conversation) {
	if uc.analysis == nil || !(account.EnableAnalysis || account.EnableAutoStaging) {
		return
	}
	if conv.AutomationEnabled != nil && !*conv.AutomationEnabled {
		return
	}
	uc.analysis.ScheduleAnalysis(conv.ID, shared.EntryTypeTelegram)
}

// recordInboundMessage persists the text and each attachment.
func (uc *HandleWebhookUseCase) recordInboundMessage(
	ctx context.Context,
	account *tgdomain.Account,
	contact *tgdomain.Contact,
	conv *tgdomain.Conversation,
	ev *tgdomain.Event,
) error {
	from := strconv.FormatInt(contact.TGUserID, 10)
	to := strconv.FormatInt(account.BotUserID, 10)
	metadata := inboundMetadata(ev)

	// `from` is a bare numeric Telegram user id, which the CRM renders verbatim
	// when nothing better is supplied. The contact is already in hand here, so
	// naming the sender costs nothing; leaving these empty is still correct, it
	// just makes the hub pay for a lookup.
	senderName, senderAvatar := contact.DisplayName(), contact.PhotoURL

	stored := uc.storeAttachments(ctx, account, conv, ev)

	// A message with neither text nor storable media still gets a row: the
	// operator must see that something arrived, especially when the reason
	// nothing was stored is that the file was too large to fetch.
	if ev.Text == "" && len(stored) == 0 {
		return uc.record(ctx, conv, conversation.MessageDirectionInbound, historyInput{
			MessageType:       conversation.MessageTypeUnsupported,
			ProviderMessageID: tgdomain.ProviderMessageID(ev.ChatID, ev.MessageID),
			From:              from,
			To:                to,
			Text:              placeholderFor(ev),
			Timestamp:         ev.Timestamp,
			Metadata:          metadata,
			SenderName:        senderName,
			SenderAvatar:      senderAvatar,
		})
	}

	if ev.Text != "" || len(stored) == 0 {
		if err := uc.record(ctx, conv, conversation.MessageDirectionInbound, historyInput{
			MessageType:       conversation.MessageTypeUserMessage,
			ProviderMessageID: tgdomain.ProviderMessageID(ev.ChatID, ev.MessageID),
			From:              from,
			To:                to,
			Text:              ev.Text,
			Timestamp:         ev.Timestamp,
			Metadata:          metadata,
			SenderName:        senderName,
			SenderAvatar:      senderAvatar,
		}); err != nil {
			return err
		}
	}

	for i, item := range stored {
		// Each attachment needs a distinct provider id or the partial unique
		// index on (entry_type, external_message_id) rejects the second one.
		providerID := tgdomain.ProviderMessageID(ev.ChatID, ev.MessageID)
		if len(stored) > 1 || ev.Text != "" {
			providerID = fmt.Sprintf("%s:att%d", providerID, i)
		}
		if err := uc.record(ctx, conv, conversation.MessageDirectionInbound, historyInput{
			MessageType:       messageTypeForMedia(item.mediaType),
			ProviderMessageID: providerID,
			From:              from,
			To:                to,
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
	return nil
}

// handleOutbound records a message the business account sent.
//
// Business mode only, and it covers BOTH our own sends through the bot and the
// owner replying from their own phone. Recording the latter is what keeps the
// CRM transcript honest when a human bypasses it. The history manager dedups on
// the provider id, so our own send being echoed here inserts nothing.
func (uc *HandleWebhookUseCase) handleOutbound(ctx context.Context, account *tgdomain.Account, ev *tgdomain.Event) error {
	_, conv, err := uc.resolveConversation(ctx, account, ev)
	if err != nil {
		return err
	}
	if err := uc.conversations.RecordOutbound(ctx, conv.ID, ev.Timestamp); err != nil {
		return err
	}

	messageType := conversation.MessageTypeOperator
	if ev.IsAutomatic {
		// An away or greeting message is Telegram's own automation, not an
		// operator's reply; labelling it as one would corrupt response-time
		// metrics.
		messageType = conversation.MessageTypeSystem
	}

	return uc.record(ctx, conv, conversation.MessageDirectionOutbound, historyInput{
		MessageType:       messageType,
		ProviderMessageID: tgdomain.ProviderMessageID(ev.ChatID, ev.MessageID),
		From:              strconv.FormatInt(account.BotUserID, 10),
		To:                strconv.FormatInt(ev.ChatID, 10),
		Text:              ev.Text,
		Timestamp:         ev.Timestamp,
		Metadata:          inboundMetadata(ev),
	})
}

// handleEdited replaces the stored text for an edited message.
func (uc *HandleWebhookUseCase) handleEdited(ctx context.Context, account *tgdomain.Account, ev *tgdomain.Event) error {
	if uc.messages == nil {
		return nil
	}
	providerID := tgdomain.ProviderMessageID(ev.ChatID, ev.MessageID)
	existing, err := uc.messages.GetByExternalMessageID(shared.EntryTypeTelegram, providerID)
	if err != nil {
		if errors.Is(err, conversation.ErrMessageNotFound) {
			return nil
		}
		return err
	}

	existing.Text = ev.Text
	existing.Metadata = mergeMetadata(existing.Metadata, map[string]any{
		"telegram_edited":    true,
		"telegram_edited_at": ev.Timestamp.UTC().Format(time.RFC3339),
	})
	if err := uc.messages.Update(existing.ID, existing); err != nil {
		return err
	}
	uc.broadcastEntryUpdate(existing.EntryID)
	return nil
}

// handleDeleted tombstones messages the business account removed.
func (uc *HandleWebhookUseCase) handleDeleted(ctx context.Context, account *tgdomain.Account, ev *tgdomain.Event) error {
	if uc.messages == nil || len(ev.DeletedMessageIDs) == 0 {
		return nil
	}
	for _, messageID := range ev.DeletedMessageIDs {
		providerID := tgdomain.ProviderMessageID(ev.ChatID, messageID)
		existing, err := uc.messages.GetByExternalMessageID(shared.EntryTypeTelegram, providerID)
		if err != nil {
			if errors.Is(err, conversation.ErrMessageNotFound) {
				continue
			}
			return err
		}
		if err := uc.messages.Delete(existing.ID); err != nil {
			return err
		}
		uc.broadcastEntryUpdate(existing.EntryID)
	}
	return nil
}

// handleCallbackQuery records an inline-keyboard tap.
//
// Answering is not optional: an unanswered callback leaves the customer's button
// spinning until it times out, so the acknowledgement is sent even if the
// bookkeeping below fails.
func (uc *HandleWebhookUseCase) handleCallbackQuery(ctx context.Context, account *tgdomain.Account, ev *tgdomain.Event) error {
	if ev.CallbackQueryID != "" && uc.api != nil {
		if err := uc.api.AnswerCallbackQuery(ctx, account.BotToken, ev.CallbackQueryID, ""); err != nil {
			log.Printf("[telegram] answerCallbackQuery failed account=@%s: %v", account.BotUsername, err)
		}
	}

	contact, conv, err := uc.resolveConversation(ctx, account, ev)
	if err != nil {
		return err
	}
	if err := uc.conversations.RecordInbound(ctx, conv.ID, ev.Timestamp); err != nil {
		return err
	}
	uc.ensureAssignment(conv, account)

	metadata, _ := json.Marshal(map[string]any{
		"telegram_callback_data": ev.CallbackData,
	})
	// The stored text is the button's LABEL, and the payload lives in metadata.
	//
	// Both are needed and they are not interchangeable. Routing keys on the
	// payload, because a label is a display string an author may reword at any
	// time. But the text is what an operator reads in the transcript and what an
	// AI agent is handed as the customer's words — and there a raw id like
	// "support" is at best unreadable and at worst actively misleading: an agent
	// whose tool description mentions "Suporte" will match it and act on it.
	if err := uc.record(ctx, conv, conversation.MessageDirectionInbound, historyInput{
		MessageType:       conversation.MessageTypeUserMessage,
		ProviderMessageID: "cb:" + ev.CallbackQueryID,
		From:              strconv.FormatInt(contact.TGUserID, 10),
		To:                strconv.FormatInt(account.BotUserID, 10),
		Text:              ev.Text,
		Timestamp:         ev.Timestamp,
		Metadata:          metadata,
		SenderName:        contact.DisplayName(),
		SenderAvatar:      contact.PhotoURL,
	}); err != nil {
		return err
	}

	if conv.IsPrivate() {
		// The message text is the label (what the contact chose, as they saw it);
		// the selection id is the payload (what the branch is labelled with). The
		// keyboard was built with that payload, so it round-trips byte-for-byte.
		uc.fireWorkflowTriggers(ctx, account, conv, ev.Text, &workflow.OptionSelection{
			ID:    ev.CallbackData,
			Title: ev.Text,
			Kind:  "callback_query",
		})
		uc.maybeReplyWithAgent(ctx, account, conv, ev.Text)
	}
	return nil
}

// handleContactShared records a consented phone share and links the CRM lead.
func (uc *HandleWebhookUseCase) handleContactShared(ctx context.Context, account *tgdomain.Account, ev *tgdomain.Event) error {
	contact, conv, err := uc.resolveConversation(ctx, account, ev)
	if err != nil {
		return err
	}
	if err := uc.conversations.RecordInbound(ctx, conv.ID, ev.Timestamp); err != nil {
		return err
	}
	uc.ensureAssignment(conv, account)

	shared_ := ev.SharedContact
	// Only a self-share links an identity. A customer can forward anyone's
	// contact card, and treating a third party's number as the sender's would
	// merge two unrelated people in the CRM.
	if shared_ != nil && ev.From != nil && shared_.UserID == ev.From.ID && shared_.PhoneNumber != "" {
		leadID := uc.resolveLead(ctx, account.WorkspaceID, shared_.PhoneNumber)
		if err := uc.contacts.SetPhone(ctx, contact.ID, shared_.PhoneNumber, leadID, ev.Timestamp); err != nil {
			log.Printf("[telegram] failed to record shared phone for contact %s: %v", contact.ID, err)
		} else {
			log.Printf("[telegram] contact %s shared their phone number (lead linked=%t)",
				contact.ID, leadID != nil)
		}
	}

	text := ev.Text
	if text == "" && shared_ != nil {
		text = strings.TrimSpace(shared_.FirstName + " " + shared_.LastName + " " + shared_.PhoneNumber)
	}
	metadata, _ := json.Marshal(map[string]any{"telegram_contact_shared": true})

	if err := uc.record(ctx, conv, conversation.MessageDirectionInbound, historyInput{
		MessageType:       conversation.MessageTypeUserMessage,
		ProviderMessageID: tgdomain.ProviderMessageID(ev.ChatID, ev.MessageID),
		From:              strconv.FormatInt(contact.TGUserID, 10),
		To:                strconv.FormatInt(account.BotUserID, 10),
		Text:              text,
		Timestamp:         ev.Timestamp,
		Metadata:          metadata,
		SenderName:        contact.DisplayName(),
		SenderAvatar:      contact.PhotoURL,
	}); err != nil {
		return err
	}

	if conv.IsPrivate() {
		uc.fireWorkflowTriggers(ctx, account, conv, text, nil)
		uc.maybeReplyWithAgent(ctx, account, conv, text)
	}
	return nil
}

func (uc *HandleWebhookUseCase) resolveLead(ctx context.Context, workspaceID, phone string) *string {
	if uc.leads == nil || phone == "" {
		return nil
	}
	leadID, err := uc.leads.FindLeadIDByPhone(ctx, workspaceID, phone)
	if err != nil || leadID == "" {
		return nil
	}
	return &leadID
}

// handleBlockToggle records that the customer blocked or unblocked the bot.
//
// In bot mode this is the entire outbound gate: there is no messaging window,
// only whether we can still reach them.
func (uc *HandleWebhookUseCase) handleBlockToggle(ctx context.Context, account *tgdomain.Account, ev *tgdomain.Event) error {
	if ev.From == nil {
		return nil
	}
	contact, err := uc.contacts.FindByTGUserID(ctx, account.ID, ev.From.ID)
	if err != nil {
		// No contact means they blocked us before ever writing; there is nothing
		// to record.
		if errors.Is(err, tgdomain.ErrContactNotFound) {
			return nil
		}
		return err
	}

	blocked := ev.Kind == tgdomain.EventBlocked
	if err := uc.contacts.SetBlocked(ctx, contact.ID, blocked, ev.Timestamp); err != nil {
		return err
	}
	log.Printf("[telegram] contact %s %s the bot @%s",
		contact.ID, map[bool]string{true: "blocked", false: "unblocked"}[blocked], account.BotUsername)

	// The composer's enabled state is derived from this flag, so the open
	// conversation has to be told.
	if conv, err := uc.conversations.FindByContact(ctx, account.ID, contact.ID); err == nil {
		uc.broadcastEntryUpdate(conv.ID)
	}
	return nil
}

// handleBusinessConnection records or updates a Telegram Business connection.
//
// The rights are stored verbatim and re-read on every such update, because the
// account owner can change or revoke them at any moment and we learn only from
// this event. Assuming the rights granted at onboarding would mean sending on
// behalf of an account that has since said no.
func (uc *HandleWebhookUseCase) handleBusinessConnection(ctx context.Context, account *tgdomain.Account, ev *tgdomain.Event) error {
	conn := ev.Connection
	if conn == nil {
		return nil
	}

	account.Mode = tgdomain.ModeBusiness
	connectionID := conn.ID
	account.BusinessConnectionID = &connectionID
	userID := conn.User.ID
	account.BusinessUserID = &userID
	account.BusinessUsername = conn.User.Username
	account.BusinessRights = conn.Rights
	account.BusinessEnabled = conn.IsEnabled

	account.Normalize()
	if err := uc.accounts.Update(ctx, account); err != nil {
		return err
	}

	log.Printf("[telegram] business connection %s for @%s: enabled=%t can_reply=%t",
		conn.ID, account.BotUsername, conn.IsEnabled, account.Rights().CanReply)
	return nil
}

// ---------------------------------------------------------------- automation

// fireWorkflowTriggers starts or advances workflows for this conversation.
//
// Gating matches the AI agent exactly — the account's workflow switch, overridden
// per conversation by the automation toggle — so pausing automation silences
// BOTH, and an operator who took over is not interrupted by a workflow step.
// fireWorkflowTriggers evaluates workflow triggers for one inbound event.
//
// sel is non-nil only when the contact TAPPED an inline button rather than
// typing. Without it a press cannot reach the option's own branch, because
// AdvanceOnReply routes on the option id and falls back to no_match.
func (uc *HandleWebhookUseCase) fireWorkflowTriggers(
	ctx context.Context,
	account *tgdomain.Account,
	conv *tgdomain.Conversation,
	text string,
	sel *workflow.OptionSelection,
) {
	if uc.workflows == nil || !account.EnableWorkflow {
		return
	}
	if conv.AutomationEnabled != nil && !*conv.AutomationEnabled {
		return
	}

	data := map[string]interface{}{
		"message":      text,
		"channel":      string(shared.EntryTypeTelegram),
		"workspace_id": account.WorkspaceID,
	}
	if account.WorkflowID != nil {
		data["account_workflow_id"] = *account.WorkflowID
	}
	workflow.ApplySelection(data, sel)

	uc.workflows.Evaluate(workflow.TriggerEvent{
		WorkspaceID: account.WorkspaceID,
		EntryID:     conv.ID,
		EntryType:   string(shared.EntryTypeTelegram),
		TriggerType: workflow.TriggerMessageReceived,
		Data:        data,
	})

	if uc.isFirstInboundMessage(conv) {
		uc.workflows.Evaluate(workflow.TriggerEvent{
			WorkspaceID: account.WorkspaceID,
			EntryID:     conv.ID,
			EntryType:   string(shared.EntryTypeTelegram),
			TriggerType: workflow.TriggerFirstMessage,
			Data:        data,
		})
	}
}

// isFirstInboundMessage reports whether the message just recorded is the first
// one this contact has sent.
//
// It counts ALL of the contact's messages, not a page of recent history. A
// windowed count is wrong in a way that only shows up deep in a conversation:
// once the bot has answered with several segments, the most recent rows are
// mostly outbound, exactly one of them is inbound, and the check reports a
// "first message" on the forty-fifth — starting a whole second workflow run.
func (uc *HandleWebhookUseCase) isFirstInboundMessage(conv *tgdomain.Conversation) bool {
	if uc.messages == nil {
		return false
	}
	// The message is already persisted, so exactly one means this is it.
	count, err := uc.messages.CountInboundByEntry(conv.ID, shared.EntryTypeTelegram)
	if err != nil {
		// Fail closed: a spurious first-message trigger starts a duplicate run
		// and sends the contact a second greeting.
		log.Printf("[telegram] could not count inbound messages for %s: %v", conv.ID, err)
		return false
	}
	return count == 1
}

// maybeReplyWithAgent hands the message to the channel-agnostic AI service,
// which owns every decision — automation gating, loop protection, the window —
// so this stays a hand-off rather than a second place where "should the bot
// answer?" is implemented.
func (uc *HandleWebhookUseCase) maybeReplyWithAgent(
	ctx context.Context,
	account *tgdomain.Account,
	conv *tgdomain.Conversation,
	text string,
) {
	if uc.aiReply == nil || account.AgentID == nil {
		return
	}
	if _, err := uc.aiReply.Reply(ctx, conversation.AIReplyRequest{
		WorkspaceID:           account.WorkspaceID,
		EntryID:               conv.ID,
		EntryType:             shared.EntryTypeTelegram,
		AgentID:               *account.AgentID,
		AgentResponsesEnabled: account.EnableAgentResponses,
		AutomationEnabled:     conv.AutomationEnabled,
		Text:                  text,
	}); err != nil {
		// An AI failure must never fail the webhook: that would redeliver a
		// message we already stored.
		log.Printf("[telegram] agent reply failed conversation=%s: %v", conv.ID, err)
	}
}

// ---------------------------------------------------------------- helpers

func (uc *HandleWebhookUseCase) resolveConversation(
	ctx context.Context,
	account *tgdomain.Account,
	ev *tgdomain.Event,
) (*tgdomain.Contact, *tgdomain.Conversation, error) {
	if ev.From == nil || ev.From.ID == 0 {
		return nil, nil, fmt.Errorf("telegram: event has no sender")
	}

	chatType := ev.ChatType
	if chatType == "" {
		chatType = tgdomain.ChatTypePrivate
	}
	chatID := ev.ChatID
	if chatID == 0 {
		chatID = ev.From.ID
	}

	contact, err := uc.contacts.FindOrCreate(ctx, tgdomain.FindOrCreateContactInput{
		WorkspaceID:  account.WorkspaceID,
		AccountID:    account.ID,
		TGUserID:     ev.From.ID,
		TGChatID:     chatID,
		ChatType:     chatType,
		Username:     ev.From.Username,
		FirstName:    ev.From.FirstName,
		LastName:     ev.From.LastName,
		LanguageCode: ev.From.LanguageCode,
		IsPremium:    ev.From.IsPremium,
	})
	if err != nil {
		return nil, nil, err
	}

	var connectionID *string
	if ev.BusinessConnectionID != "" {
		id := ev.BusinessConnectionID
		connectionID = &id
	}

	conv, err := uc.conversations.FindOrCreate(ctx, tgdomain.FindOrCreateConversationInput{
		WorkspaceID:          account.WorkspaceID,
		AccountID:            account.ID,
		ContactID:            contact.ID,
		TGChatID:             chatID,
		ChatType:             chatType,
		BusinessConnectionID: connectionID,
	})
	if err != nil {
		return nil, nil, err
	}
	return contact, conv, nil
}

// bindDeepLink resolves a /start payload and stamps the attribution.
func (uc *HandleWebhookUseCase) bindDeepLink(
	ctx context.Context,
	account *tgdomain.Account,
	conv *tgdomain.Conversation,
	ev *tgdomain.Event,
) {
	if uc.deepLinks == nil || ev.StartPayload == "" {
		return
	}
	link, err := uc.deepLinks.FindByToken(ctx, ev.StartPayload)
	if err != nil {
		// An unknown or stale token is not an error: links are shared publicly and
		// can outlive their campaign. The conversation still opens.
		log.Printf("[telegram] unknown start payload %q for @%s", ev.StartPayload, account.BotUsername)
		return
	}
	if link.AccountID != account.ID || link.Expired(time.Now().UTC()) {
		return
	}

	if err := uc.conversations.SetStartPayload(ctx, conv.ID, link.Token); err != nil {
		log.Printf("[telegram] failed to stamp start payload on conversation %s: %v", conv.ID, err)
	}
	if err := uc.deepLinks.MarkUsed(ctx, link.Token, ev.Timestamp); err != nil {
		log.Printf("[telegram] failed to count deep link use %s: %v", link.Token, err)
	}
	log.Printf("[telegram] conversation %s attributed to deep link %s (%s)", conv.ID, link.Token, link.Label)
}

func (uc *HandleWebhookUseCase) ensureAssignment(conv *tgdomain.Conversation, account *tgdomain.Account) {
	if uc.assignments == nil {
		return
	}
	// The account id takes the business-phone slot so each connected bot keeps
	// its own round-robin pool.
	uc.assignments.EnsureAssignment(conv.ID, string(shared.EntryTypeTelegram), account.ID)
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

	// SenderName/SenderAvatar label the live broadcast. They are filled from the
	// contact the inbound path has already loaded, so the websocket event is
	// correct without the hub re-reading the contact from the database.
	SenderName   string
	SenderAvatar string
}

// record persists and broadcasts through the SHARED history manager, so Telegram
// reuses the same dedup, persistence and websocket fan-out as every other
// channel instead of reimplementing them.
func (uc *HandleWebhookUseCase) record(
	ctx context.Context,
	conv *tgdomain.Conversation,
	direction conversation.MessageHistoryDirection,
	in historyInput,
) error {
	if uc.history == nil {
		return nil
	}
	return uc.history.Record(ctx, direction, conversation.MessageHistoryRecord{
		EntryID:           conv.ID,
		EntryType:         shared.EntryTypeTelegram,
		Channel:           conversation.MessageChannelTelegram,
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
	uc.broadcaster.BroadcastEntryUpdate(entryID, string(shared.EntryTypeTelegram), nil)
}

// storedAttachment is a downloaded attachment persisted to object storage.
type storedAttachment struct {
	mediaID   string
	mediaType conversation.MediaType
	url       string
}

// storeAttachments downloads each attachment and persists it.
//
// The >20MB case is the channel's hardest product limit: bots simply cannot
// download such a file, so the attempt is skipped entirely rather than made and
// failed. The message still gets a row (see recordInboundMessage), which is what
// turns an invisible gap in the transcript into a visible placeholder.
func (uc *HandleWebhookUseCase) storeAttachments(
	ctx context.Context,
	account *tgdomain.Account,
	conv *tgdomain.Conversation,
	ev *tgdomain.Event,
) []storedAttachment {
	if uc.fileStorage == nil || uc.api == nil || len(ev.Attachments) == 0 {
		return nil
	}

	out := make([]storedAttachment, 0, len(ev.Attachments))
	for _, att := range ev.Attachments {
		if att.FileID == "" {
			continue
		}
		if att.TooLarge {
			log.Printf("[telegram] attachment too large to download (%d bytes > %d) account=@%s chat=%d",
				att.Size, tgdomain.MaxDownloadBytes, account.BotUsername, ev.ChatID)
			continue
		}

		file, err := uc.api.GetFile(ctx, account.BotToken, att.FileID)
		if err != nil {
			log.Printf("[telegram] getFile failed kind=%s: %v", att.Kind, err)
			continue
		}
		if file.TooLarge {
			log.Printf("[telegram] attachment too large to download (%d bytes) account=@%s",
				file.Size, account.BotUsername)
			continue
		}

		data, contentType, err := uc.api.DownloadFile(ctx, account.BotToken, file.Path)
		if err != nil {
			log.Printf("[telegram] download failed kind=%s: %v", att.Kind, err)
			continue
		}
		// getFile "may not preserve the original file name and MIME type", so the
		// type captured from the webhook wins over whatever the download reported.
		if att.MIMEType != "" {
			contentType = att.MIMEType
		}

		mediaID := uuid.NewString()
		key := fmt.Sprintf("conversations/%s/%s/%s%s",
			shared.EntryTypeTelegram, conv.ID, mediaID, extensionFor(contentType, att.FileName, file.Path))

		if err := uc.fileStorage.UploadFile(key, data); err != nil {
			log.Printf("[telegram] attachment upload failed key=%s: %v", key, err)
			continue
		}
		url := uc.fileStorage.GetFileURL(key)

		mediaType := conversationMediaType(att.Kind)
		if uc.convMedia != nil {
			record := &conversation.ConversationMedia{
				ID:        mediaID,
				EntryID:   conv.ID,
				EntryType: shared.EntryTypeTelegram,
				Type:      mediaType,
				MimeType:  contentType,
				URL:       url,
				SizeBytes: int64(len(data)),
			}
			record.Normalize()
			if err := record.Validate(); err == nil {
				if err := uc.convMedia.Create(record); err != nil {
					log.Printf("[telegram] conversation media insert failed id=%s: %v", mediaID, err)
				}
			}
		}

		out = append(out, storedAttachment{mediaID: mediaID, mediaType: mediaType, url: url})
	}
	return out
}

// enrichContact refreshes a stale profile.
//
// Only the avatar actually needs fetching — Telegram already put the name,
// username and locale in the update — so this is deliberately rare.
func (uc *HandleWebhookUseCase) enrichContact(ctx context.Context, account *tgdomain.Account, contact *tgdomain.Contact) {
	if uc.api == nil || !contact.ProfileIsStale(time.Now().UTC(), profileTTL) {
		return
	}

	profile := tgdomain.ContactProfile{
		Username:     contact.Username,
		FirstName:    contact.FirstName,
		LastName:     contact.LastName,
		LanguageCode: contact.LanguageCode,
		IsPremium:    contact.IsPremium,
		FetchedAt:    time.Now().UTC(),
	}

	if fileID, err := uc.api.GetUserProfilePhotoFileID(ctx, account.BotToken, contact.TGUserID); err == nil && fileID != "" {
		profile.PhotoFileID = fileID
		// The avatar is re-hosted rather than linked: Telegram's download URL is
		// guaranteed valid only "for at least 1 hour", so a stored link would rot.
		if url := uc.storeAvatar(ctx, account, contact, fileID); url != "" {
			profile.PhotoURL = url
		}
	}

	if err := uc.contacts.UpdateProfile(ctx, contact.ID, profile); err != nil {
		// Enrichment is cosmetic; a failure must never drop a message.
		log.Printf("[telegram] profile update failed contact=%s: %v", contact.ID, err)
	}
}

func (uc *HandleWebhookUseCase) storeAvatar(ctx context.Context, account *tgdomain.Account, contact *tgdomain.Contact, fileID string) string {
	if uc.fileStorage == nil {
		return ""
	}
	file, err := uc.api.GetFile(ctx, account.BotToken, fileID)
	if err != nil || file.TooLarge {
		return ""
	}
	data, contentType, err := uc.api.DownloadFile(ctx, account.BotToken, file.Path)
	if err != nil {
		return ""
	}
	key := fmt.Sprintf("contacts/%s/%s/avatar%s",
		shared.EntryTypeTelegram, contact.ID, extensionFor(contentType, "", file.Path))
	if err := uc.fileStorage.UploadFile(key, data); err != nil {
		return ""
	}
	return uc.fileStorage.GetFileURL(key)
}

// ---------------------------------------------------------------- mapping

func inboundMetadata(ev *tgdomain.Event) json.RawMessage {
	meta := map[string]any{
		"telegram_message_id": ev.MessageID,
		"telegram_chat_id":    ev.ChatID,
	}
	if ev.ReplyToMessageID != 0 {
		meta["telegram_reply_to_message_id"] = ev.ReplyToMessageID
	}
	if ev.MediaGroupID != "" {
		// Albums arrive as separate updates sharing this id; the UI groups them.
		meta["telegram_media_group_id"] = ev.MediaGroupID
	}
	if ev.StartPayload != "" {
		meta["telegram_start_payload"] = ev.StartPayload
	}
	if ev.Location != nil {
		meta["telegram_latitude"] = ev.Location.Latitude
		meta["telegram_longitude"] = ev.Location.Longitude
	}
	if ev.IsAutomatic {
		meta["telegram_from_offline"] = true
	}
	for _, att := range ev.Attachments {
		if att.Emoji != "" {
			meta["telegram_sticker_emoji"] = att.Emoji
		}
		if att.TooLarge {
			meta["telegram_file_too_large"] = true
			meta["telegram_file_size"] = att.Size
		}
	}

	raw, err := json.Marshal(meta)
	if err != nil {
		return nil
	}
	return raw
}

// placeholderFor is what the operator sees for a message we could not render.
//
// The too-large case is stated plainly rather than hidden: it is a real platform
// limit, and an operator who knows a file exists can open Telegram to see it.
func placeholderFor(ev *tgdomain.Event) string {
	for _, att := range ev.Attachments {
		if att.TooLarge {
			return fmt.Sprintf("[file too large to download — %s, open in Telegram]", humanSize(att.Size))
		}
		if att.Emoji != "" {
			return att.Emoji
		}
	}
	if ev.Location != nil {
		return "[location]"
	}
	return "[unsupported message]"
}

func humanSize(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGT"[exp])
}

func conversationMediaType(kind tgdomain.MediaKind) conversation.MediaType {
	switch kind {
	case tgdomain.MediaPhoto:
		return conversation.MediaTypeImage
	case tgdomain.MediaVideo:
		return conversation.MediaTypeVideo
	case tgdomain.MediaAudio, tgdomain.MediaVoice:
		return conversation.MediaTypeAudio
	default:
		return conversation.MediaTypeDocument
	}
}

// messageTypeForMedia keeps audio distinguishable, because the speech-to-text
// pipeline keys on it.
func messageTypeForMedia(mediaType conversation.MediaType) conversation.MessageType {
	if mediaType == conversation.MediaTypeAudio {
		return conversation.MessageTypeAudio
	}
	return conversation.MessageTypeMedia
}

func extensionFor(contentType, fileName, filePath string) string {
	if fileName != "" {
		if ext := path.Ext(fileName); ext != "" {
			return ext
		}
	}
	if contentType != "" {
		if exts, err := mime.ExtensionsByType(contentType); err == nil && len(exts) > 0 {
			return exts[0]
		}
	}
	if ext := path.Ext(filePath); ext != "" {
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
