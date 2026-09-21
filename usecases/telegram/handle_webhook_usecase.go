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

const profileTTL = 7 * 24 * time.Hour

var ErrUnknownAccount = errors.New("telegram: webhook for an unknown account")

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

type LeadLinker interface {
	FindLeadIDByPhone(ctx context.Context, workspaceID, phone string) (string, error)
}

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

type QueuedUpdate struct {
	AccountID string          `json:"account_id"`
	Update    json.RawMessage `json:"update"`
}

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

func (uc *HandleWebhookUseCase) resolveAccount(ctx context.Context, accountID string, u *tgdomain.Update) (*tgdomain.Account, error) {
	if connectionID := businessConnectionIDOf(u); connectionID != "" {
		account, err := uc.accounts.FindByBusinessConnectionID(ctx, connectionID)
		if err == nil {
			return account, nil
		}
		if !errors.Is(err, tgdomain.ErrAccountNotFound) {
			return nil, err
		}
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
		log.Printf("[telegram] unhandled update kind account=@%s raw=%s",
			account.BotUsername, truncateRaw(ev.Raw, 512))
		return nil
	}
	return nil
}

func (uc *HandleWebhookUseCase) handleInbound(ctx context.Context, account *tgdomain.Account, ev *tgdomain.Event) error {
	contact, conv, err := uc.resolveConversation(ctx, account, ev)
	if err != nil {
		return err
	}

	private := conv.IsPrivate()

	if contact.Blocked {
		if err := uc.contacts.SetBlocked(ctx, contact.ID, false, ev.Timestamp); err == nil {
			contact.Blocked = false
		}
	}

	if err := uc.conversations.RecordInbound(ctx, conv.ID, ev.Timestamp); err != nil {
		return err
	}
	if private {
		uc.ensureAssignment(conv, account)
	}

	uc.bindDeepLink(ctx, account, conv, ev)

	if err := uc.recordInboundMessage(ctx, account, contact, conv, ev); err != nil {
		return err
	}

	uc.enrichContact(ctx, account, contact)

	if private {
		uc.fireWorkflowTriggers(ctx, account, conv, ev.Text, nil)
		uc.maybeReplyWithAgent(ctx, account, contact, conv, ev.Text)
		uc.scheduleAnalysis(account, conv)
	}
	return nil
}

func (uc *HandleWebhookUseCase) scheduleAnalysis(account *tgdomain.Account, conv *tgdomain.Conversation) {
	if uc.analysis == nil || !(account.EnableAnalysis || account.EnableAutoStaging || account.EnableAutoMemory) {
		return
	}
	if conv.AutomationEnabled != nil && !*conv.AutomationEnabled {
		return
	}
	uc.analysis.ScheduleAnalysis(conv.ID, shared.EntryTypeTelegram)
}

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

	senderName, senderAvatar := contact.DisplayName(), contact.PhotoURL

	stored := uc.storeAttachments(ctx, account, conv, ev)

	if ev.Text == "" && len(stored) == 0 {
		return uc.record(ctx, conv, conversation.MessageDirectionInbound, historyInput{
			MessageType:       conversation.MessageTypeUnsupported,
			ProviderMessageID: tgdomain.ProviderMessageID(account.BotUserID, ev.ChatID, ev.MessageID),
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
			ProviderMessageID: tgdomain.ProviderMessageID(account.BotUserID, ev.ChatID, ev.MessageID),
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
		providerID := tgdomain.ProviderMessageID(account.BotUserID, ev.ChatID, ev.MessageID)
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
		messageType = conversation.MessageTypeSystem
	}

	return uc.record(ctx, conv, conversation.MessageDirectionOutbound, historyInput{
		MessageType:       messageType,
		ProviderMessageID: tgdomain.ProviderMessageID(account.BotUserID, ev.ChatID, ev.MessageID),
		From:              strconv.FormatInt(account.BotUserID, 10),
		To:                strconv.FormatInt(ev.ChatID, 10),
		Text:              ev.Text,
		Timestamp:         ev.Timestamp,
		Metadata:          inboundMetadata(ev),
	})
}

func (uc *HandleWebhookUseCase) handleEdited(ctx context.Context, account *tgdomain.Account, ev *tgdomain.Event) error {
	if uc.messages == nil {
		return nil
	}
	providerID := tgdomain.ProviderMessageID(account.BotUserID, ev.ChatID, ev.MessageID)
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

func (uc *HandleWebhookUseCase) handleDeleted(ctx context.Context, account *tgdomain.Account, ev *tgdomain.Event) error {
	if uc.messages == nil || len(ev.DeletedMessageIDs) == 0 {
		return nil
	}
	for _, messageID := range ev.DeletedMessageIDs {
		providerID := tgdomain.ProviderMessageID(account.BotUserID, ev.ChatID, messageID)
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
		uc.fireWorkflowTriggers(ctx, account, conv, ev.Text, &workflow.OptionSelection{
			ID:    ev.CallbackData,
			Title: ev.Text,
			Kind:  "callback_query",
		})
		uc.maybeReplyWithAgent(ctx, account, contact, conv, ev.Text)
	}
	return nil
}

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
		ProviderMessageID: tgdomain.ProviderMessageID(account.BotUserID, ev.ChatID, ev.MessageID),
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
		uc.maybeReplyWithAgent(ctx, account, contact, conv, text)
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

func (uc *HandleWebhookUseCase) handleBlockToggle(ctx context.Context, account *tgdomain.Account, ev *tgdomain.Event) error {
	if ev.From == nil {
		return nil
	}
	contact, err := uc.contacts.FindByTGUserID(ctx, account.ID, ev.From.ID)
	if err != nil {
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

	if conv, err := uc.conversations.FindByContact(ctx, account.ID, contact.ID); err == nil {
		uc.broadcastEntryUpdate(conv.ID)
	}
	return nil
}

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
	workflow.ApplyContactNumber(data, strconv.FormatInt(conv.TGChatID, 10))

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

func (uc *HandleWebhookUseCase) isFirstInboundMessage(conv *tgdomain.Conversation) bool {
	if uc.messages == nil {
		return false
	}
	count, err := uc.messages.CountInboundByEntry(conv.ID, shared.EntryTypeTelegram)
	if err != nil {
		log.Printf("[telegram] could not count inbound messages for %s: %v", conv.ID, err)
		return false
	}
	return count == 1
}

func (uc *HandleWebhookUseCase) maybeReplyWithAgent(
	ctx context.Context,
	account *tgdomain.Account,
	contact *tgdomain.Contact,
	conv *tgdomain.Conversation,
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
		EntryType:             shared.EntryTypeTelegram,
		AgentID:               *account.AgentID,
		AgentResponsesEnabled: account.EnableAgentResponses,
		AutomationEnabled:     conv.AutomationEnabled,
		Text:                  text,
		LeadID:                leadID,
	}); err != nil {
		log.Printf("[telegram] agent reply failed conversation=%s: %v", conv.ID, err)
	}
}

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
	uc.assignments.EnsureAssignment(conv.ID, string(shared.EntryTypeTelegram), account.ID)
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

type storedAttachment struct {
	mediaID   string
	mediaType conversation.MediaType
	url       string
}

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
		if att.MIMEType != "" {
			contentType = att.MIMEType
		}

		mediaID := uuid.NewString()
		key := fmt.Sprintf("conversations/%s/%s/%s%s",
			shared.EntryTypeTelegram, conv.ID, mediaID, extensionFor(contentType, att.FileName, file.Path))

		if err := uc.fileStorage.UploadFile(key, data, contentType); err != nil {
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
		if url := uc.storeAvatar(ctx, account, contact, fileID); url != "" {
			profile.PhotoURL = url
		}
	}

	if err := uc.contacts.UpdateProfile(ctx, contact.ID, profile); err != nil {
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
	if err := uc.fileStorage.UploadFile(key, data, contentType); err != nil {
		return ""
	}
	return uc.fileStorage.GetFileURL(key)
}

func inboundMetadata(ev *tgdomain.Event) json.RawMessage {
	meta := map[string]any{
		"telegram_message_id": ev.MessageID,
		"telegram_chat_id":    ev.ChatID,
	}
	if ev.ReplyToMessageID != 0 {
		meta["telegram_reply_to_message_id"] = ev.ReplyToMessageID
	}
	if ev.MediaGroupID != "" {
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

func placeholderFor(ev *tgdomain.Event) string {
	for _, att := range ev.Attachments {
		if att.TooLarge {
			return fmt.Sprintf("[file too large to download, %s, open in Telegram]", humanSize(att.Size))
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
