package instagram

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"vozko/domain/channel"
	"vozko/domain/conversation"
	igdomain "vozko/domain/instagram"
	"vozko/domain/shared"
	"vozko/infra/meta"
)

// channelAdapter is the Instagram implementation of conversation.ChannelAdapter.
//
// It is what makes a reply leave from the SAME account the message arrived on:
// ResolveEntry walks entry -> conversation -> account, and every send uses that
// account's own IG id and token. With several accounts connected to one
// workspace, nothing else keeps them apart.
type channelAdapter struct {
	accounts      igdomain.AccountRepository
	contacts      igdomain.ContactRepository
	conversations igdomain.ConversationRepository
	messaging     igdomain.MessagingService

	caps channel.Capabilities
}

// NewChannelAdapter builds the Instagram send adapter.
func NewChannelAdapter(
	accounts igdomain.AccountRepository,
	contacts igdomain.ContactRepository,
	conversations igdomain.ConversationRepository,
	messaging igdomain.MessagingService,
) conversation.ChannelAdapter {
	return &channelAdapter{
		accounts:      accounts,
		contacts:      contacts,
		conversations: conversations,
		messaging:     messaging,
		caps:          igdomain.Descriptor().Capabilities,
	}
}

func (a *channelAdapter) EntryType() shared.EntryType { return shared.EntryTypeInstagram }

// ResolveEntry loads the account and contact behind an entry id.
func (a *channelAdapter) ResolveEntry(ctx context.Context, entryID string) (*conversation.EntryContext, error) {
	conv, err := a.conversations.FindByID(ctx, entryID)
	if err != nil {
		return nil, err
	}
	account, err := a.accounts.FindByID(ctx, conv.IGAccountID)
	if err != nil {
		return nil, err
	}
	contact, err := a.contacts.FindByID(ctx, conv.ContactID)
	if err != nil {
		return nil, err
	}

	return &conversation.EntryContext{
		EntryID:       conv.ID,
		EntryType:     shared.EntryTypeInstagram,
		WorkspaceID:   conv.WorkspaceID,
		AccountID:     account.ID,
		ContactID:     contact.ID,
		ContactRef:    contact.IGSID,
		ContactHandle: contact.Username,
		LastInboundAt: conv.LastCustomerMessageAt,
	}, nil
}

// WindowState reports whether the 24h window is open.
//
// The window is a sliding deadline anchored on the contact's last inbound
// message, so it reopens every time they write to us. Reporting expiresAt lets
// the UI explain *why* the composer is disabled instead of failing the send.
func (a *channelAdapter) WindowState(ctx context.Context, ec *conversation.EntryContext) (bool, *time.Time, error) {
	if ec == nil {
		return false, nil, conversation.ErrNoAdapterForEntryType
	}
	if ec.LastInboundAt == nil {
		// No inbound message ever: Instagram forbids initiating a conversation.
		return false, nil, nil
	}
	expires := ec.LastInboundAt.Add(a.caps.OutboundWindow)
	return time.Now().UTC().Before(expires), &expires, nil
}

func (a *channelAdapter) SendText(ctx context.Context, ec *conversation.EntryContext, req conversation.SendTextRequest) (*conversation.SendOutcome, error) {
	account, err := a.sendableAccount(ctx, ec)
	if err != nil {
		return nil, err
	}
	if err := a.assertWindowOpen(ctx, ec); err != nil {
		return nil, err
	}
	// Enforce the documented BYTE limit here as well as in the client so the
	// error surfaces as a domain error rather than an API rejection.
	if len(req.Body) > a.caps.MaxTextBytes {
		return nil, igdomain.ErrTextTooLong
	}

	result, err := a.messaging.SendText(ctx, account.IGUserID, account.AccessToken, igdomain.SendTextInput{
		RecipientIGSID: ec.ContactRef,
		Text:           req.Body,
		ReplyToMID:     req.ReplyToProviderMessageID,
	})
	if err != nil {
		log.Printf("[instagram] send text FAILED account=@%s (ig_user_id=%s) recipient=%s: %v",
			account.Username, account.IGUserID, ec.ContactRef, err)
		return nil, a.classify(ctx, account, err)
	}
	log.Printf("[instagram] sent text account=@%s recipient=%s mid=%s",
		account.Username, ec.ContactRef, result.MessageID)

	a.recordOutbound(ctx, ec)
	return &conversation.SendOutcome{ProviderMessageID: result.MessageID}, nil
}

func (a *channelAdapter) SendMedia(ctx context.Context, ec *conversation.EntryContext, req conversation.SendMediaRequest) (*conversation.SendOutcome, error) {
	account, err := a.sendableAccount(ctx, ec)
	if err != nil {
		return nil, err
	}
	if err := a.assertWindowOpen(ctx, ec); err != nil {
		return nil, err
	}
	// Instagram fetches the asset server-side, so raw bytes cannot be sent.
	if req.URL == "" {
		return nil, fmt.Errorf("%w: instagram media must be sent as a publicly reachable URL",
			conversation.ErrCapabilityUnsupported)
	}
	if err := a.validateMedia(req); err != nil {
		return nil, err
	}

	result, err := a.messaging.SendMedia(ctx, account.IGUserID, account.AccessToken, igdomain.SendMediaInput{
		RecipientIGSID: ec.ContactRef,
		Kind:           req.Kind,
		URL:            req.URL,
		ReplyToMID:     req.ReplyToProviderMessageID,
	})
	if err != nil {
		log.Printf("[instagram] send %s FAILED account=@%s recipient=%s: %v",
			req.Kind, account.Username, ec.ContactRef, err)
		return nil, a.classify(ctx, account, err)
	}
	log.Printf("[instagram] sent %s account=@%s recipient=%s mid=%s",
		req.Kind, account.Username, ec.ContactRef, result.MessageID)

	a.recordOutbound(ctx, ec)
	return &conversation.SendOutcome{ProviderMessageID: result.MessageID}, nil
}

// SendReaction implements conversation.ReactingAdapter.
//
// Note that Instagram never echoes our own reactions back, so the caller must
// record them locally — no webhook will confirm this.
func (a *channelAdapter) SendReaction(ctx context.Context, ec *conversation.EntryContext, targetProviderMessageID, reaction string) error {
	account, err := a.sendableAccount(ctx, ec)
	if err != nil {
		return err
	}
	if err := a.messaging.SendReaction(ctx, account.IGUserID, account.AccessToken,
		ec.ContactRef, targetProviderMessageID, reaction); err != nil {
		return a.classify(ctx, account, err)
	}
	return nil
}

func (a *channelAdapter) RemoveReaction(ctx context.Context, ec *conversation.EntryContext, targetProviderMessageID string) error {
	account, err := a.sendableAccount(ctx, ec)
	if err != nil {
		return err
	}
	if err := a.messaging.RemoveReaction(ctx, account.IGUserID, account.AccessToken,
		ec.ContactRef, targetProviderMessageID); err != nil {
		return a.classify(ctx, account, err)
	}
	return nil
}

// SendTyping implements conversation.PresenceAdapter.
func (a *channelAdapter) SendTyping(ctx context.Context, ec *conversation.EntryContext, on bool) error {
	account, err := a.sendableAccount(ctx, ec)
	if err != nil {
		return err
	}
	if err := a.messaging.SendTyping(ctx, account.IGUserID, account.AccessToken, ec.ContactRef, on); err != nil {
		return a.classify(ctx, account, err)
	}
	return nil
}

// MarkSeen sends a read receipt. Instagram has no watermark, so the argument is
// accepted for interface symmetry but the provider marks the thread, not a range.
func (a *channelAdapter) MarkSeen(ctx context.Context, ec *conversation.EntryContext, _ string) error {
	account, err := a.sendableAccount(ctx, ec)
	if err != nil {
		return err
	}
	if err := a.messaging.MarkSeen(ctx, account.IGUserID, account.AccessToken, ec.ContactRef); err != nil {
		return a.classify(ctx, account, err)
	}
	return nil
}

// ---------------------------------------------------------------- internals

func (a *channelAdapter) sendableAccount(ctx context.Context, ec *conversation.EntryContext) (*igdomain.Account, error) {
	if ec == nil || ec.AccountID == "" {
		return nil, conversation.ErrNoAdapterForEntryType
	}
	account, err := a.accounts.FindByID(ctx, ec.AccountID)
	if err != nil {
		return nil, err
	}
	if !account.CanReceiveMessages() {
		return nil, fmt.Errorf("instagram account %s cannot send (status=%s, messaging scope granted=%t)",
			account.Username, account.Status, account.HasScope(igdomain.ScopeManageMessages))
	}
	if account.AccessToken == "" {
		return nil, igdomain.ErrAccessTokenRequired
	}
	return account, nil
}

func (a *channelAdapter) assertWindowOpen(ctx context.Context, ec *conversation.EntryContext) error {
	open, _, err := a.WindowState(ctx, ec)
	if err != nil {
		return err
	}
	if !open {
		return conversation.ErrOutboundWindowClosed
	}
	return nil
}

// validateMedia checks kind, MIME type and size against the channel descriptor
// before spending a Graph call. Note images cap at 8MB while audio/video/pdf cap
// at 25MB, and gif is unsupported.
func (a *channelAdapter) validateMedia(req conversation.SendMediaRequest) error {
	kind := channel.MediaKind(req.Kind)
	limit, ok := a.caps.MediaLimits[kind]
	if !ok {
		return fmt.Errorf("%w: instagram does not support media kind %q",
			conversation.ErrCapabilityUnsupported, req.Kind)
	}
	if req.MIMEType != "" && !limit.Allows(req.MIMEType) {
		return fmt.Errorf("%w: instagram does not accept %s for %s",
			conversation.ErrCapabilityUnsupported, req.MIMEType, req.Kind)
	}
	if size := int64(len(req.Bytes)); size > 0 && size > limit.MaxBytes {
		return fmt.Errorf("%w: %s exceeds instagram's %d byte limit",
			conversation.ErrCapabilityUnsupported, req.Kind, limit.MaxBytes)
	}
	return nil
}

// recordOutbound advances the agent clock. Best effort: the message is already
// delivered, so a bookkeeping failure must not surface as a send failure.
func (a *channelAdapter) recordOutbound(ctx context.Context, ec *conversation.EntryContext) {
	_ = a.conversations.RecordOutbound(ctx, ec.EntryID, time.Now().UTC())
}

// classify maps a Graph failure onto a domain error and reacts to a dead token.
//
// A revoked or expired token is not transient, so the account is marked
// immediately: that is what turns an invisible "messages stopped working" into a
// visible Reconnect prompt.
func (a *channelAdapter) classify(ctx context.Context, account *igdomain.Account, err error) error {
	apiErr, ok := meta.AsError(err)
	if !ok {
		return err
	}

	switch {
	case apiErr.NeedsReauth():
		if account.Status.CanTransitionTo(igdomain.StatusTokenExpired) {
			_ = a.accounts.UpdateStatus(ctx, account.ID, igdomain.StatusTokenExpired,
				"token rejected by Instagram; reconnect required")
		}
		return fmt.Errorf("instagram account %s needs to be reconnected: %w", account.Username, err)

	case apiErr.IsWindowClosed():
		return errors.Join(conversation.ErrOutboundWindowClosed, err)

	case apiErr.IsRecipientUnreachable():
		return fmt.Errorf("instagram recipient is unreachable (blocked or deactivated): %w", err)
	}
	return err
}
