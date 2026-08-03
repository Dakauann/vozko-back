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

// channelAdapter is the Telegram implementation of conversation.ChannelAdapter.
//
// It is what makes a reply leave from the SAME bot the message arrived on:
// ResolveEntry walks entry → conversation → account, and every send uses that
// account's own token. With several bots connected to one workspace, nothing
// else keeps them apart.
type channelAdapter struct {
	accounts      tgdomain.AccountRepository
	contacts      tgdomain.ContactRepository
	conversations tgdomain.ConversationRepository
	files         tgdomain.FileCacheRepository
	api           tgdomain.BotAPI

	caps channel.Capabilities
}

// NewChannelAdapter builds the Telegram send adapter.
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

// ResolveEntry loads the account and contact behind an entry id.
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
		EntryID:     conv.ID,
		EntryType:   shared.EntryTypeTelegram,
		WorkspaceID: conv.WorkspaceID,
		AccountID:   account.ID,
		ContactID:   contact.ID,
		// The chat id, not the user id: they are equal in a private chat but
		// diverge for groups, and a group whose id migrated would otherwise be
		// unreachable.
		ContactRef:    strconv.FormatInt(conv.TGChatID, 10),
		ContactHandle: contact.Handle(),
		LastInboundAt: conv.LastCustomerMessageAt,
	}, nil
}

// WindowState reports whether an outbound message is allowed right now.
//
// This is the one place the two connection modes genuinely differ, and it is why
// Telegram needs no second entry type:
//
//   - BOT mode has no messaging window at all. A bot cannot OPEN a conversation,
//     but every conversation we hold was opened by the customer, so the only
//     thing that can close the composer is the customer blocking the bot.
//   - BUSINESS mode reintroduces Instagram's exact 24h rule, because can_reply
//     is defined as "the bot can send and edit messages in the private chats
//     that had incoming messages in the last 24 hours".
//
// Both the send path and the composer's UI state consult this, so there is
// exactly one definition of "can I reply right now".
func (a *channelAdapter) WindowState(ctx context.Context, ec *conversation.EntryContext) (bool, *time.Time, error) {
	if ec == nil {
		return false, nil, conversation.ErrNoAdapterForEntryType
	}
	account, err := a.accounts.FindByID(ctx, ec.AccountID)
	if err != nil {
		return false, nil, err
	}

	if account.Mode == tgdomain.ModeBusiness {
		if !account.BusinessEnabled || !account.Rights().CanReply {
			// The owner disconnected the bot or revoked the reply right. No clock
			// is involved, so no expiry is reported, the UI must say "permission
			// revoked", not "window closed".
			return false, nil, nil
		}
		if ec.LastInboundAt == nil {
			return false, nil, nil
		}
		expires := ec.LastInboundAt.Add(tgdomain.BusinessMessagingWindow)
		return time.Now().UTC().Before(expires), &expires, nil
	}

	// Bot mode: the gate is reachability, not time.
	contact, err := a.contacts.FindByID(ctx, ec.ContactID)
	if err != nil {
		return false, nil, err
	}
	// A nil expiry with open=false is what tells the UI "this is not a clock",
	// there is no moment at which it reopens on its own, only the customer
	// unblocking us.
	return !contact.Blocked, nil, nil
}

func (a *channelAdapter) SendText(ctx context.Context, ec *conversation.EntryContext, req conversation.SendTextRequest) (*conversation.SendOutcome, error) {
	account, conv, err := a.sendable(ctx, ec)
	if err != nil {
		return nil, err
	}
	// Telegram counts CHARACTERS, unlike Instagram's byte limit. Capabilities
	// applies whichever the channel declares, so no caller has to remember which.
	if a.caps.TextTooLong(req.Body) {
		return nil, tgdomain.ErrTextTooLong
	}

	in := tgdomain.SendTextInput{
		ChatID: conv.TGChatID,
		// HTML rather than MarkdownV2: MarkdownV2 requires escaping a long list
		// of characters, and a single stray underscore in a customer's name fails
		// the whole send.
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

	// The provider id is known synchronously, Telegram answers a send with the
	// full Message, so there is no echo webhook to reconcile against, unlike
	// every Meta channel.
	return &conversation.SendOutcome{
		ProviderMessageID: tgdomain.ProviderMessageID(result.ChatID, result.MessageID),
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

	// A previously uploaded asset is re-sent by file_id, which has NO size limit
	// and costs no upload at all. This is what makes a repeated boleto image or
	// logo free, and it sidesteps the URL-send size caps entirely.
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
		ProviderMessageID: tgdomain.ProviderMessageID(result.ChatID, result.MessageID),
	}, nil
}

// SendReaction implements conversation.ReactingAdapter.
//
// Bots may set at most one reaction per message. Note this is outbound only:
// receiving a customer's reaction requires the bot to be "an administrator in
// the chat", which does not exist in a private chat, so nothing is promised on
// the inbound side.
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
	// An empty emoji clears the reaction, the same call, no separate method.
	return a.SendReaction(ctx, ec, targetProviderMessageID, "")
}

// SendTyping implements conversation.PresenceAdapter.
//
// The indicator expires in five seconds or less, so a caller showing it across a
// slow AI turn must re-issue it rather than set it once.
func (a *channelAdapter) SendTyping(ctx context.Context, ec *conversation.EntryContext, on bool) error {
	if !on {
		// There is no "stop typing" call: the status clears itself, and clears
		// immediately when a message arrives from the bot.
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

// MarkSeen marks the customer's message read on the account owner's behalf.
//
// Business mode only, and only with the can_read_messages right, a bot has no
// read receipts of its own. Bot mode reports the capability as unsupported
// rather than silently doing nothing, so a caller cannot believe it worked.
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

// EditText implements conversation.EditingAdapter.
//
// Telegram is the only channel we carry where an operator can fix a message
// already sent. Neither WhatsApp nor Instagram permits it, which is why this is
// an OPTIONAL capability discovered by type assertion rather than a method on
// every adapter.
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

// Retract implements conversation.RetractingAdapter, a real unsend.
//
// Bounded by Telegram's rule: "A message can only be deleted if it was sent less
// than 48 hours ago." The bound is enforced here as well as upstream so the
// error names the reason instead of surfacing an opaque Bad Request.
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

// ---------------------------------------------------------------- internals

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

	open, _, err := a.WindowState(ctx, ec)
	if err != nil {
		return nil, nil, err
	}
	if !open {
		return nil, nil, conversation.ErrOutboundWindowClosed
	}
	return account, conv, nil
}

// businessConnectionOf returns the connection to send on behalf of, or "" in bot
// mode. The conversation's own connection wins: an account may have been
// reconnected, and a conversation must keep answering on the connection it
// arrived through.
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

// validateMedia checks kind, MIME type and size against the descriptor before
// spending an API call.
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

// mediaKindFor picks the send method.
//
// A voice note is not just "audio": sendVoice renders an in-chat waveform, and
// Telegram requires audio/ogg for it, anything else "will be sent as files". So
// the ogg check decides the method rather than the caller.
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

// recordOutbound advances the agent clock. Best effort: the message is already
// delivered, so a bookkeeping failure must not surface as a send failure.
func (a *channelAdapter) recordOutbound(ctx context.Context, ec *conversation.EntryContext) {
	_ = a.conversations.RecordOutbound(ctx, ec.EntryID, time.Now().UTC())
}

// classify maps a Bot API failure onto a domain error and reacts to it.
//
// Telegram gives more usable failure information than Meta does, and all three
// cases below are actionable rather than merely loggable:
//
//   - 401 means the token was revoked in BotFather; the account is marked so the
//     UI shows Reconnect instead of failing silently forever.
//   - 403 means the customer blocked the bot; the contact is flagged so the
//     composer explains itself.
//   - migrate_to_chat_id means the group became a supergroup and has a NEW id;
//     rewriting it is the only thing that keeps the conversation alive.
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
		// Rewriting both rows is what makes the NEXT send work. Not doing it
		// leaves the conversation permanently unreachable with no visible cause.
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
