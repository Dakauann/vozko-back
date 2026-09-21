package telegram

import (
	"context"
	"errors"
	"fmt"
	"html"
	"log"
	"strconv"
	"time"

	"vozko/domain/channel"
	"vozko/domain/conversation"
	"vozko/domain/shared"
	tgdomain "vozko/domain/telegram"
)

type channelAdapter struct {
	accounts      tgdomain.AccountRepository
	contacts      tgdomain.ContactRepository
	conversations tgdomain.ConversationRepository
	files         tgdomain.FileCacheRepository
	api           tgdomain.BotAPI

	caps channel.Capabilities
}

func NewChannelAdapter(
	accounts tgdomain.AccountRepository,
	contacts tgdomain.ContactRepository,
	conversations tgdomain.ConversationRepository,
	files tgdomain.FileCacheRepository,
	api tgdomain.BotAPI,
) conversation.ChannelAdapter {
	return &channelAdapter{
		accounts:      accounts,
		contacts:      contacts,
		conversations: conversations,
		files:         files,
		api:           api,
		caps:          tgdomain.Descriptor().Capabilities,
	}
}

func (a *channelAdapter) EntryType() shared.EntryType { return shared.EntryTypeTelegram }

func (a *channelAdapter) ResolveEntry(ctx context.Context, entryID string) (*conversation.EntryContext, error) {
	conv, err := a.conversations.FindByID(ctx, entryID)
	if err != nil {
		return nil, err
	}
	account, err := a.accounts.FindByID(ctx, conv.AccountID)
	if err != nil {
		return nil, err
	}
	contact, err := a.contacts.FindByID(ctx, conv.ContactID)
	if err != nil {
		return nil, err
	}

	return &conversation.EntryContext{
		EntryID:       conv.ID,
		EntryType:     shared.EntryTypeTelegram,
		WorkspaceID:   conv.WorkspaceID,
		AccountID:     account.ID,
		ContactID:     contact.ID,
		ContactRef:    strconv.FormatInt(conv.TGChatID, 10),
		ContactHandle: contact.Handle(),
		LastInboundAt: conv.LastCustomerMessageAt,
	}, nil
}

func (a *channelAdapter) WindowState(ctx context.Context, ec *conversation.EntryContext) (conversation.WindowState, error) {
	if ec == nil {
		return conversation.ClosedWindow(conversation.WindowReasonChannelUnavailable), conversation.ErrNoAdapterForEntryType
	}
	account, err := a.accounts.FindByID(ctx, ec.AccountID)
	if err != nil {
		return conversation.ClosedWindow(conversation.WindowReasonChannelUnavailable), err
	}

	if account.Mode == tgdomain.ModeBusiness {
		if !account.BusinessEnabled || !account.Rights().CanReply {
			return conversation.ClosedWindow(conversation.WindowReasonReplyRevoked), nil
		}
		if ec.LastInboundAt == nil {
			return conversation.ClosedWindow(conversation.WindowReasonNoInbound), nil
		}
		expires := ec.LastInboundAt.Add(tgdomain.BusinessMessagingWindow)
		if time.Now().UTC().Before(expires) {
			return conversation.OpenWindow(&expires), nil
		}
		return conversation.ClosedWindow(conversation.WindowReasonExpired), nil
	}

	contact, err := a.contacts.FindByID(ctx, ec.ContactID)
	if err != nil {
		return conversation.ClosedWindow(conversation.WindowReasonChannelUnavailable), err
	}
	if contact.Blocked {
		return conversation.ClosedWindow(conversation.WindowReasonContactBlocked), nil
	}
	return conversation.OpenWindow(nil), nil
}

func (a *channelAdapter) SendText(ctx context.Context, ec *conversation.EntryContext, req conversation.SendTextRequest) (*conversation.SendOutcome, error) {
	account, conv, err := a.sendable(ctx, ec)
	if err != nil {
		return nil, err
	}
	if a.caps.TextTooLong(req.Body) {
		return nil, tgdomain.ErrTextTooLong
	}

	in := tgdomain.SendTextInput{
		ChatID:               conv.TGChatID,
		Text:                 html.EscapeString(req.Body),
		ParseMode:            "HTML",
		BusinessConnectionID: businessConnectionOf(account, conv),
	}
	if req.ReplyToProviderMessageID != "" {
		if _, messageID, ok := tgdomain.ParseProviderMessageID(req.ReplyToProviderMessageID); ok {
			in.ReplyToMessageID = messageID
		}
	}

	result, err := a.api.SendText(ctx, account.BotToken, in)
	if err != nil {
		return nil, a.classify(ctx, account, conv, ec, err)
	}

	log.Printf("[telegram] sent text account=@%s chat=%d message_id=%d",
		account.BotUsername, result.ChatID, result.MessageID)
	a.recordOutbound(ctx, ec)

	return &conversation.SendOutcome{
		ProviderMessageID: tgdomain.ProviderMessageID(account.BotUserID, result.ChatID, result.MessageID),
	}, nil
}

func (a *channelAdapter) SendMedia(ctx context.Context, ec *conversation.EntryContext, req conversation.SendMediaRequest) (*conversation.SendOutcome, error) {
	account, conv, err := a.sendable(ctx, ec)
	if err != nil {
		return nil, err
	}
	if err := a.validateMedia(req); err != nil {
		return nil, err
	}

	kind := mediaKindFor(req.Kind, req.MIMEType)
	in := tgdomain.SendMediaInput{
		ChatID:               conv.TGChatID,
		Kind:                 kind,
		URL:                  req.URL,
		Bytes:                req.Bytes,
		FileName:             req.FileName,
		MIMEType:             req.MIMEType,
		BusinessConnectionID: businessConnectionOf(account, conv),
	}
	if req.Caption != "" {
		in.Caption = html.EscapeString(req.Caption)
	}
	if req.ReplyToProviderMessageID != "" {
		if _, messageID, ok := tgdomain.ParseProviderMessageID(req.ReplyToProviderMessageID); ok {
			in.ReplyToMessageID = messageID
		}
	}

	cacheKey := req.URL
	if cacheKey != "" && a.files != nil {
		if fileID, err := a.files.Get(ctx, account.ID, cacheKey); err == nil && fileID != "" {
			in.FileID = fileID
			in.URL = ""
		}
	}

	result, err := a.api.SendMedia(ctx, account.BotToken, in)
	if err != nil {
		return nil, a.classify(ctx, account, conv, ec, err)
	}

	if a.files != nil && cacheKey != "" && result.FileID != "" && in.FileID == "" {
		if err := a.files.Put(ctx, account.ID, cacheKey, result.FileID); err != nil {
			log.Printf("[telegram] failed to cache file id: %v", err)
		}
	}

	log.Printf("[telegram] sent %s account=@%s chat=%d message_id=%d",
		kind, account.BotUsername, result.ChatID, result.MessageID)
	a.recordOutbound(ctx, ec)

	return &conversation.SendOutcome{
		ProviderMessageID: tgdomain.ProviderMessageID(account.BotUserID, result.ChatID, result.MessageID),
	}, nil
}

func (a *channelAdapter) SendReaction(ctx context.Context, ec *conversation.EntryContext, targetProviderMessageID, reaction string) error {
	account, conv, err := a.sendable(ctx, ec)
	if err != nil {
		return err
	}
	_, messageID, ok := tgdomain.ParseProviderMessageID(targetProviderMessageID)
	if !ok {
		return fmt.Errorf("%w: unrecognised message id %q", conversation.ErrCapabilityUnsupported, targetProviderMessageID)
	}
	if err := a.api.SetMessageReaction(ctx, account.BotToken, conv.TGChatID, messageID, reaction); err != nil {
		return a.classify(ctx, account, conv, ec, err)
	}
	return nil
}

func (a *channelAdapter) RemoveReaction(ctx context.Context, ec *conversation.EntryContext, targetProviderMessageID string) error {
	return a.SendReaction(ctx, ec, targetProviderMessageID, "")
}

func (a *channelAdapter) SendTyping(ctx context.Context, ec *conversation.EntryContext, on bool) error {
	if !on {
		return nil
	}
	account, conv, err := a.sendable(ctx, ec)
	if err != nil {
		return err
	}
	if err := a.api.SendChatAction(ctx, account.BotToken, conv.TGChatID,
		tgdomain.ActionTyping, businessConnectionOf(account, conv)); err != nil {
		return a.classify(ctx, account, conv, ec, err)
	}
	return nil
}

func (a *channelAdapter) MarkSeen(ctx context.Context, ec *conversation.EntryContext, upToProviderMessageID string) error {
	account, conv, err := a.sendable(ctx, ec)
	if err != nil {
		return err
	}
	if account.Mode != tgdomain.ModeBusiness {
		return fmt.Errorf("%w: telegram read receipts require a business connection",
			conversation.ErrCapabilityUnsupported)
	}
	if !account.Rights().CanReadMessages {
		return fmt.Errorf("%w: the business connection does not grant can_read_messages",
			conversation.ErrCapabilityUnsupported)
	}
	_, messageID, ok := tgdomain.ParseProviderMessageID(upToProviderMessageID)
	if !ok {
		return nil
	}
	if err := a.api.ReadBusinessMessage(ctx, account.BotToken,
		businessConnectionOf(account, conv), conv.TGChatID, messageID); err != nil {
		return a.classify(ctx, account, conv, ec, err)
	}
	return nil
}

func (a *channelAdapter) EditText(ctx context.Context, ec *conversation.EntryContext, providerMessageID, body string) error {
	account, conv, err := a.sendable(ctx, ec)
	if err != nil {
		return err
	}
	if a.caps.TextTooLong(body) {
		return tgdomain.ErrTextTooLong
	}
	_, messageID, ok := tgdomain.ParseProviderMessageID(providerMessageID)
	if !ok {
		return fmt.Errorf("%w: unrecognised message id %q", conversation.ErrCapabilityUnsupported, providerMessageID)
	}
	if err := a.api.EditText(ctx, account.BotToken, conv.TGChatID, messageID,
		html.EscapeString(body), "HTML", businessConnectionOf(account, conv)); err != nil {
		return a.classify(ctx, account, conv, ec, err)
	}
	return nil
}

func (a *channelAdapter) Retract(ctx context.Context, ec *conversation.EntryContext, providerMessageID string, sentAt time.Time) error {
	account, conv, err := a.sendable(ctx, ec)
	if err != nil {
		return err
	}
	if !sentAt.IsZero() && time.Since(sentAt) > tgdomain.DeleteWindow {
		return fmt.Errorf("%w: telegram only allows deleting messages sent in the last 48 hours",
			conversation.ErrCapabilityUnsupported)
	}
	_, messageID, ok := tgdomain.ParseProviderMessageID(providerMessageID)
	if !ok {
		return fmt.Errorf("%w: unrecognised message id %q", conversation.ErrCapabilityUnsupported, providerMessageID)
	}

	if account.Mode == tgdomain.ModeBusiness {
		return a.api.DeleteBusinessMessages(ctx, account.BotToken,
			businessConnectionOf(account, conv), []int64{messageID})
	}
	if err := a.api.DeleteMessage(ctx, account.BotToken, conv.TGChatID, messageID); err != nil {
		return a.classify(ctx, account, conv, ec, err)
	}
	return nil
}

func (a *channelAdapter) sendable(ctx context.Context, ec *conversation.EntryContext) (*tgdomain.Account, *tgdomain.Conversation, error) {
	if ec == nil || ec.AccountID == "" || ec.EntryID == "" {
		return nil, nil, conversation.ErrNoAdapterForEntryType
	}
	account, err := a.accounts.FindByID(ctx, ec.AccountID)
	if err != nil {
		return nil, nil, err
	}
	if !account.CanSend() {
		if account.Mode == tgdomain.ModeBusiness && !account.Rights().CanReply {
			return nil, nil, tgdomain.ErrCannotReply
		}
		return nil, nil, fmt.Errorf("telegram bot @%s cannot send (status=%s)",
			account.BotUsername, account.Status)
	}
	conv, err := a.conversations.FindByID(ctx, ec.EntryID)
	if err != nil {
		return nil, nil, err
	}

	window, err := a.WindowState(ctx, ec)
	if err != nil {
		return nil, nil, err
	}
	if !window.Open {
		return nil, nil, conversation.ErrOutboundWindowClosed
	}
	return account, conv, nil
}

func businessConnectionOf(account *tgdomain.Account, conv *tgdomain.Conversation) string {
	if account.Mode != tgdomain.ModeBusiness {
		return ""
	}
	if conv.BusinessConnectionID != nil && *conv.BusinessConnectionID != "" {
		return *conv.BusinessConnectionID
	}
	if account.BusinessConnectionID != nil {
		return *account.BusinessConnectionID
	}
	return ""
}

func (a *channelAdapter) validateMedia(req conversation.SendMediaRequest) error {
	kind := channel.MediaKind(req.Kind)
	limit, ok := a.caps.MediaLimits[kind]
	if !ok {
		return fmt.Errorf("%w: telegram does not support media kind %q",
			conversation.ErrCapabilityUnsupported, req.Kind)
	}
	if req.MIMEType != "" && !limit.Allows(req.MIMEType) {
		return fmt.Errorf("%w: telegram does not accept %s for %s",
			conversation.ErrCapabilityUnsupported, req.MIMEType, req.Kind)
	}
	if size := int64(len(req.Bytes)); size > 0 && size > limit.MaxBytes {
		return fmt.Errorf("%w: %s exceeds telegram's %d byte upload limit",
			conversation.ErrCapabilityUnsupported, req.Kind, limit.MaxBytes)
	}
	if req.URL == "" && len(req.Bytes) == 0 {
		return fmt.Errorf("%w: telegram media needs a URL or bytes",
			conversation.ErrCapabilityUnsupported)
	}
	return nil
}

func mediaKindFor(kind, mimeType string) tgdomain.MediaKind {
	switch kind {
	case "image":
		return tgdomain.MediaPhoto
	case "video":
		return tgdomain.MediaVideo
	case "audio":
		if mimeType == "audio/ogg" {
			return tgdomain.MediaVoice
		}
		return tgdomain.MediaAudio
	default:
		return tgdomain.MediaDocument
	}
}

func (a *channelAdapter) recordOutbound(ctx context.Context, ec *conversation.EntryContext) {
	_ = a.conversations.RecordOutbound(ctx, ec.EntryID, time.Now().UTC())
}

func (a *channelAdapter) classify(
	ctx context.Context,
	account *tgdomain.Account,
	conv *tgdomain.Conversation,
	ec *conversation.EntryContext,
	err error,
) error {
	var apiErr *tgdomain.APIError
	if !errors.As(err, &apiErr) {
		return err
	}

	switch {
	case apiErr.NeedsReconnect():
		if account.Status.CanTransitionTo(tgdomain.StatusTokenInvalid) {
			_ = a.accounts.UpdateStatus(ctx, account.ID, tgdomain.StatusTokenInvalid,
				"the bot token was revoked in BotFather; reconnect with a new token")
		}
		return fmt.Errorf("telegram bot @%s needs to be reconnected: %w", account.BotUsername, err)

	case apiErr.BlockedByUser():
		if ec != nil && ec.ContactID != "" {
			_ = a.contacts.SetBlocked(ctx, ec.ContactID, true, time.Now().UTC())
		}
		return errors.Join(conversation.ErrOutboundWindowClosed,
			fmt.Errorf("%w: %s", tgdomain.ErrContactBlocked, apiErr.Description))

	case apiErr.Migrated():
		log.Printf("[telegram] chat %d migrated to %d; rewriting conversation %s",
			conv.TGChatID, apiErr.MigrateToChatID, conv.ID)
		_ = a.conversations.UpdateChatID(ctx, conv.ID, apiErr.MigrateToChatID)
		if ec != nil && ec.ContactID != "" {
			_ = a.contacts.UpdateChatID(ctx, ec.ContactID, apiErr.MigrateToChatID)
		}
		return fmt.Errorf("telegram chat migrated to a supergroup; retry the send: %w", err)
	}
	return err
}
