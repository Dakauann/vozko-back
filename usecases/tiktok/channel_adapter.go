package tiktok

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"
	"unicode/utf8"

	"vozko/domain/channel"
	"vozko/domain/conversation"
	"vozko/domain/shared"
	ttdomain "vozko/domain/tiktok"
)

const cardFallbackTitle = "Escolha uma opção:"

const negativeImageCapabilityFreshness = time.Hour

type ChannelAdapterDeps struct {
	Accounts      ttdomain.AccountRepository
	Contacts      ttdomain.ContactRepository
	Conversations ttdomain.ConversationRepository
	Tokens        *TokenSource
	Messaging     ttdomain.MessagingService
	Errors        ttdomain.ErrorClassifier
	Fetcher       MediaFetcher
	Side          *SideMessages
	Metrics       Metrics
}

type ChannelAdapter struct {
	d    ChannelAdapterDeps
	caps channel.Capabilities
	now  Clock
}

func NewChannelAdapter(d ChannelAdapterDeps) *ChannelAdapter {
	if d.Accounts == nil || d.Contacts == nil || d.Conversations == nil || d.Tokens == nil || d.Messaging == nil || d.Errors == nil || d.Fetcher == nil || d.Side == nil || d.Metrics == nil {
		panic("tiktok: the channel adapter requires accounts, contacts, conversations, tokens, messaging, an error classifier, a media fetcher, side messages and metrics")
	}
	return &ChannelAdapter{d: d, caps: ttdomain.ChannelCapabilities(), now: utcNow}
}

var (
	_ conversation.ChannelAdapter      = (*ChannelAdapter)(nil)
	_ conversation.PresenceAdapter     = (*ChannelAdapter)(nil)
	_ conversation.InteractiveAdapter  = (*ChannelAdapter)(nil)
	_ conversation.WindowStatesAdapter = (*ChannelAdapter)(nil)
	_ conversation.CapabilitiesAdapter = (*ChannelAdapter)(nil)
)

func (a *ChannelAdapter) EntryType() shared.EntryType { return shared.EntryTypeTikTok }

func (a *ChannelAdapter) Capabilities() channel.Capabilities { return a.caps }

func (a *ChannelAdapter) InteractiveLimits() channel.InteractiveLimits { return a.caps.Interactive }

func (a *ChannelAdapter) ResolveEntry(ctx context.Context, entryID string) (*conversation.EntryContext, error) {
	conv, err := a.d.Conversations.FindByID(ctx, entryID)
	if err != nil {
		return nil, err
	}
	contact, err := a.d.Contacts.FindByID(ctx, conv.ContactID)
	if err != nil {
		return nil, err
	}
	return &conversation.EntryContext{
		EntryID:       conv.ID,
		EntryType:     shared.EntryTypeTikTok,
		WorkspaceID:   conv.WorkspaceID,
		AccountID:     conv.AccountID,
		ContactID:     contact.ID,
		ContactRef:    contact.UniqueIdentifier,
		ContactHandle: contact.Name(),
		LastInboundAt: conv.LastCustomerMessageAt,
	}, nil
}

func (a *ChannelAdapter) WindowState(ctx context.Context, ec *conversation.EntryContext) (conversation.WindowState, error) {
	if ec == nil {
		return conversation.ClosedWindow(conversation.WindowReasonChannelUnavailable), conversation.ErrNoAdapterForEntryType
	}
	conv, err := a.d.Conversations.FindByID(ctx, ec.EntryID)
	if err != nil {
		return conversation.ClosedWindow(conversation.WindowReasonChannelUnavailable), err
	}
	account, err := a.d.Accounts.FindByID(ctx, conv.AccountID)
	if err != nil {
		return conversation.ClosedWindow(conversation.WindowReasonChannelUnavailable), err
	}
	return windowOf(account, conv, a.now()), nil
}

func (a *ChannelAdapter) AutomationDisclosure(ctx context.Context, ec *conversation.EntryContext) (string, error) {
	if ec == nil {
		return "", conversation.ErrNoAdapterForEntryType
	}
	account, err := a.d.Accounts.FindByID(ctx, ec.AccountID)
	if err != nil {
		return "", err
	}
	return account.Automation().Disclosure, nil
}

var _ conversation.DisclosingAdapter = (*ChannelAdapter)(nil)

func (a *ChannelAdapter) WindowStates(ctx context.Context, entryIDs []string) (map[string]conversation.WindowState, error) {
	convs, err := a.d.Conversations.FindByIDs(ctx, shared.UniqueNonEmpty(entryIDs))
	if err != nil {
		return nil, err
	}
	accounts := map[string]*ttdomain.Account{}
	now := a.now()
	out := make(map[string]conversation.WindowState, len(convs))
	for _, conv := range convs {
		account, ok := accounts[conv.AccountID]
		if !ok {
			found, err := a.d.Accounts.FindByID(ctx, conv.AccountID)
			if err != nil && !errors.Is(err, ttdomain.ErrAccountNotFound) {
				return nil, err
			}
			accounts[conv.AccountID], account = found, found
		}
		out[conv.ID] = windowOf(account, conv, now)
	}
	return out, nil
}

func windowOf(account *ttdomain.Account, conv *ttdomain.Conversation, now time.Time) conversation.WindowState {
	if account == nil || !account.CanSend() {
		return conversation.ClosedWindow(conversation.WindowReasonChannelUnavailable)
	}
	return conv.Window(now)
}

type sendable struct {
	account *ttdomain.Account
	conv    *ttdomain.Conversation
	creds   ttdomain.Credentials
}

func (a *ChannelAdapter) sendable(ctx context.Context, ec *conversation.EntryContext) (*sendable, error) {
	if ec == nil {
		return nil, conversation.ErrNoAdapterForEntryType
	}
	conv, err := a.d.Conversations.FindByID(ctx, ec.EntryID)
	if err != nil {
		return nil, err
	}
	account, err := a.d.Accounts.FindByID(ctx, conv.AccountID)
	if err != nil {
		return nil, err
	}
	if state := windowOf(account, conv, a.now()); !state.Open {
		return nil, fmt.Errorf("%w: %s", conversation.ErrOutboundWindowClosed, state.Reason)
	}
	creds, err := a.d.Tokens.Credentials(ctx, account)
	if err != nil {
		return nil, errors.Join(conversation.ErrOutboundWindowClosed, err)
	}
	return &sendable{account: account, conv: conv, creds: creds}, nil
}

func (a *ChannelAdapter) SendText(ctx context.Context, ec *conversation.EntryContext, req conversation.SendTextRequest) (*conversation.SendOutcome, error) {
	body := strings.TrimSpace(req.Body)
	if body == "" {
		return nil, ttdomain.ErrTextEmpty
	}
	if a.caps.TextTooLong(body) {
		return nil, ttdomain.ErrTextTooLong
	}
	s, err := a.sendable(ctx, ec)
	if err != nil {
		return nil, err
	}
	return a.claimed(ctx, s, func() (string, error) {
		return a.d.Messaging.SendText(ctx, s.creds, s.conv.ConversationID, body, req.ReplyToProviderMessageID)
	})
}

func (a *ChannelAdapter) SendMedia(ctx context.Context, ec *conversation.EntryContext, req conversation.SendMediaRequest) (*conversation.SendOutcome, error) {
	if req.Kind != string(channel.MediaImage) {
		return nil, fmt.Errorf("%w: %s", ttdomain.ErrMediaUnsupported, req.Kind)
	}
	data, mimeType, err := a.imageBytes(ctx, req)
	if err != nil {
		return nil, err
	}
	s, err := a.sendable(ctx, ec)
	if err != nil {
		return nil, err
	}
	if err := a.imagesAllowed(ctx, s); err != nil {
		return nil, err
	}
	if caption := strings.TrimSpace(req.Caption); caption != "" {
		if a.caps.TextTooLong(caption) {
			return nil, ttdomain.ErrTextTooLong
		}
		if err := a.sideText(ctx, s, caption, req.ReplyToProviderMessageID); err != nil {
			return nil, err
		}
	}
	return a.claimed(ctx, s, func() (string, error) {
		mediaID, err := a.d.Messaging.UploadImage(ctx, s.creds, data, req.FileName, mimeType)
		if err != nil {
			return "", err
		}
		return a.d.Messaging.SendImage(ctx, s.creds, s.conv.ConversationID, mediaID)
	})
}

func (a *ChannelAdapter) imageBytes(ctx context.Context, req conversation.SendMediaRequest) ([]byte, string, error) {
	data := req.Bytes
	if len(data) == 0 {
		if strings.TrimSpace(req.URL) == "" {
			return nil, "", fmt.Errorf("%w: an image needs bytes or a url", conversation.ErrCapabilityUnsupported)
		}
		fetched, err := a.d.Fetcher.Fetch(ctx, req.URL)
		if err != nil {
			return nil, "", err
		}
		data = fetched
	}
	limit := a.caps.MediaLimits[channel.MediaImage]
	if int64(len(data)) > limit.MaxBytes {
		return nil, "", ttdomain.ErrMediaTooLarge
	}
	mimeType := strings.TrimSpace(strings.Split(req.MIMEType, ";")[0])
	if !limit.Allows(mimeType) {
		return nil, "", fmt.Errorf("%w: %s", ttdomain.ErrMediaUnsupported, mimeType)
	}
	return data, mimeType, nil
}

func (a *ChannelAdapter) imagesAllowed(ctx context.Context, s *sendable) error {
	now := a.now()
	if allowed, known := s.conv.ImageSendKnown(now); known {
		if allowed {
			return nil
		}
		if s.conv.ImageSendCheckedAt != nil && now.Sub(*s.conv.ImageSendCheckedAt) < negativeImageCapabilityFreshness {
			return ttdomain.ErrImageNotAllowedInMarket
		}
	}
	allowed, err := a.d.Messaging.ImageAllowed(ctx, s.creds, s.conv.ConversationID, s.conv.ConversationType)
	if err != nil {
		a.d.Metrics.AddTikTokEvent("send", "image_capability_unknown")
		return fmt.Errorf("%w: tiktok did not confirm images for this conversation: %w", conversation.ErrCapabilityUnsupported, err)
	}
	if err := a.d.Conversations.SetImageCapability(ctx, s.conv.ID, allowed, now); err != nil {
		log.Printf("[tiktok] image capability of %s not cached: %v", s.conv.ID, err)
	}
	if !allowed {
		return ttdomain.ErrImageNotAllowedInMarket
	}
	return nil
}

func (a *ChannelAdapter) SendInteractive(ctx context.Context, ec *conversation.EntryContext, req conversation.SendInteractiveRequest) (*conversation.SendOutcome, error) {
	body := strings.TrimSpace(req.ComposedBody())
	if body == "" {
		return nil, fmt.Errorf("%w: an interactive prompt needs a body", conversation.ErrCapabilityUnsupported)
	}
	if a.caps.TextTooLong(body) {
		return nil, ttdomain.ErrTextTooLong
	}
	kept, dropped := conversation.FitOptions(req.Options, a.caps.Interactive.MaxOptionsButtons, a.caps.Interactive.MaxPayloadBytes)
	if len(kept) == 0 {
		return nil, fmt.Errorf("%w: no option fits a tiktok card", ttdomain.ErrInteractiveUnsupported)
	}
	for _, d := range dropped {
		log.Printf("[tiktok] option %q omitted: %s", d.ID, d.Reason)
	}
	card := ttdomain.Card{Title: body}
	for _, opt := range kept {
		label, clipped := shared.TruncateRunes(opt.Title, a.caps.Interactive.MaxLabelRunes)
		if clipped {
			log.Printf("[tiktok] option %q label clipped to %d characters", opt.ID, a.caps.Interactive.MaxLabelRunes)
		}
		card.Buttons = append(card.Buttons, ttdomain.CardButton{ID: opt.ID, Text: label})
	}
	s, err := a.sendable(ctx, ec)
	if err != nil {
		return nil, err
	}
	if utf8.RuneCountInString(body) > ttdomain.MaxCardTitleRunes {
		if err := a.sideText(ctx, s, body, ""); err != nil {
			return nil, err
		}
		card.Title = cardFallbackTitle
	}
	return a.claimed(ctx, s, func() (string, error) {
		return a.d.Messaging.SendCard(ctx, s.creds, s.conv.ConversationID, card)
	})
}

func (a *ChannelAdapter) claimed(ctx context.Context, s *sendable, send func() (string, error)) (*conversation.SendOutcome, error) {
	now := a.now()
	ok, err := a.d.Conversations.ClaimOutbound(ctx, s.conv.ID, now)
	if err != nil {
		a.d.Metrics.AddTikTokEvent("send", "claim_failed")
		return nil, fmt.Errorf("tiktok: the message budget could not be claimed, nothing was sent: %w", err)
	}
	if !ok {
		a.d.Metrics.AddTikTokEvent("send", "budget_exhausted")
		return nil, fmt.Errorf("%w: %s", conversation.ErrOutboundWindowClosed, conversation.WindowReasonBudgetExhausted)
	}
	messageID, err := send()
	if err != nil {
		return nil, a.failed(ctx, s, err)
	}
	if err := a.d.Conversations.RecordOutbound(ctx, s.conv.ID, now); err != nil {
		a.d.Metrics.AddTikTokEvent("send", "clock_not_advanced")
		log.Printf("[tiktok] outbound clock of %s not advanced: %v", s.conv.ID, err)
	}
	a.d.Metrics.AddTikTokEvent("send", "sent")
	return &conversation.SendOutcome{ProviderMessageID: messageID, DeliveryStatus: conversation.DeliveryStatusSent}, nil
}

func (a *ChannelAdapter) failed(ctx context.Context, s *sendable, err error) error {
	switch {
	case a.d.Errors.IsBudgetBlocked(err):
		a.d.Metrics.AddTikTokEvent("send", "blocked_by_tiktok")
		if closeErr := a.d.Conversations.CloseBudget(ctx, s.conv.ID, a.now()); closeErr != nil {
			log.Printf("[tiktok] budget close of %s not saved: %v", s.conv.ID, closeErr)
		}
		return errors.Join(fmt.Errorf("%w: %s", conversation.ErrOutboundWindowClosed, conversation.WindowReasonBudgetExhausted), err)
	case a.d.Errors.IsDefiniteRefusal(err):
		if releaseErr := a.d.Conversations.ReleaseOutbound(ctx, s.conv.ID); releaseErr != nil {
			log.Printf("[tiktok] budget claim of %s not released: %v", s.conv.ID, releaseErr)
		}
		if a.d.Errors.IsAuth(err) {
			a.d.Metrics.AddTikTokEvent("send", "auth_refused")
			return errors.Join(conversation.ErrOutboundWindowClosed, ttdomain.ErrNeedsReconnect, err)
		}
		a.d.Metrics.AddTikTokEvent("send", "refused")
		return err
	}
	a.d.Metrics.AddTikTokEvent("send", "ambiguous")
	return err
}

func (a *ChannelAdapter) SendTyping(ctx context.Context, ec *conversation.EntryContext, on bool) error {
	if !on {
		return nil
	}
	return a.senderAction(ctx, ec, ttdomain.SenderTyping)
}

func (a *ChannelAdapter) MarkSeen(ctx context.Context, ec *conversation.EntryContext, _ string) error {
	return a.senderAction(ctx, ec, ttdomain.SenderMarkRead)
}

func (a *ChannelAdapter) senderAction(ctx context.Context, ec *conversation.EntryContext, action ttdomain.SenderAction) error {
	s, err := a.sendable(ctx, ec)
	if err != nil {
		return err
	}
	return a.d.Messaging.SendAction(ctx, s.creds, s.conv.ConversationID, action)
}

func (a *ChannelAdapter) sideText(ctx context.Context, s *sendable, text, replyTo string) error {
	outcome, err := a.claimed(ctx, s, func() (string, error) {
		return a.d.Messaging.SendText(ctx, s.creds, s.conv.ConversationID, text, replyTo)
	})
	if err != nil {
		return err
	}
	if err := a.d.Side.Mark(outcome.ProviderMessageID); err != nil {
		log.Printf("[tiktok] side message %s not marked, its echo may repeat the text: %v", outcome.ProviderMessageID, err)
	}
	return nil
}
