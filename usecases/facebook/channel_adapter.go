package facebook

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"vozko/domain/channel"
	"vozko/domain/conversation"
	fbdomain "vozko/domain/facebook"
	"vozko/domain/shared"
)

type ChannelAdapterDeps struct {
	Pages              fbdomain.PageRepository
	Contacts           fbdomain.ContactRepository
	Conversations      fbdomain.ConversationRepository
	Messaging          fbdomain.MessagingService
	Routing            fbdomain.RoutingService
	HumanAgentApproved bool
}

type ChannelAdapter struct {
	d    ChannelAdapterDeps
	caps channel.Capabilities
	now  func() time.Time
}

func NewChannelAdapter(d ChannelAdapterDeps) *ChannelAdapter {
	return &ChannelAdapter{
		d:    d,
		caps: fbdomain.Descriptor(d.HumanAgentApproved).Capabilities,
		now:  func() time.Time { return time.Now().UTC() },
	}
}

var (
	_ conversation.ChannelAdapter     = (*ChannelAdapter)(nil)
	_ conversation.ReactingAdapter    = (*ChannelAdapter)(nil)
	_ conversation.PresenceAdapter    = (*ChannelAdapter)(nil)
	_ conversation.InteractiveAdapter = (*ChannelAdapter)(nil)
)

func (a *ChannelAdapter) EntryType() shared.EntryType { return shared.EntryTypeFacebook }

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
		EntryType:     shared.EntryTypeFacebook,
		WorkspaceID:   conv.WorkspaceID,
		AccountID:     conv.PageID,
		ContactID:     contact.ID,
		ContactRef:    contact.PSID,
		ContactHandle: contact.DisplayName(),
		LastInboundAt: conv.LastCustomerMessageAt,
	}, nil
}

func (a *ChannelAdapter) WindowState(_ context.Context, ec *conversation.EntryContext) (conversation.WindowState, error) {
	if ec == nil {
		return conversation.ClosedWindow(conversation.WindowReasonChannelUnavailable), conversation.ErrNoAdapterForEntryType
	}
	return conversation.EvaluateWindow(a.caps, ec.LastInboundAt, a.now(), true), nil
}

func (a *ChannelAdapter) SendText(ctx context.Context, ec *conversation.EntryContext, req conversation.SendTextRequest) (*conversation.SendOutcome, error) {
	if a.caps.TextTooLong(req.Body) {
		return nil, fbdomain.ErrTextTooLong
	}
	target, err := a.target(ctx, ec, req.HumanInitiated)
	if err != nil {
		return nil, err
	}
	return a.dispatch(ctx, target, ec, fbdomain.OutboundMessage{Text: req.Body, ReplyToMID: req.ReplyToProviderMessageID})
}

func (a *ChannelAdapter) SendMedia(ctx context.Context, ec *conversation.EntryContext, req conversation.SendMediaRequest) (*conversation.SendOutcome, error) {
	if err := a.validateMedia(req); err != nil {
		return nil, err
	}
	caption := strings.TrimSpace(req.Caption)
	if a.caps.TextTooLong(caption) {
		return nil, fbdomain.ErrTextTooLong
	}
	target, err := a.target(ctx, ec, req.HumanInitiated)
	if err != nil {
		return nil, err
	}
	if caption != "" {
		if _, err := a.dispatch(ctx, target.withMetadata(fbdomain.CaptionMetadata), ec, fbdomain.OutboundMessage{Text: caption}); err != nil {
			return nil, err
		}
	}

	msg := fbdomain.OutboundMessage{AttachmentKind: req.Kind, AttachmentURL: req.URL, ReplyToMID: req.ReplyToProviderMessageID}
	if len(req.Bytes) > 0 {
		attachmentID, err := a.d.Messaging.Upload(ctx, target.page.FBPageID, target.page.PageToken, fbdomain.UploadInput{
			Kind: req.Kind, Bytes: req.Bytes, MIMEType: req.MIMEType, FileName: req.FileName,
		})
		if err != nil {
			return nil, a.classify(ctx, target.page, ec, err)
		}
		msg.AttachmentURL, msg.AttachmentID = "", attachmentID
	}
	return a.dispatch(ctx, target, ec, msg)
}

func (a *ChannelAdapter) SendInteractive(ctx context.Context, ec *conversation.EntryContext, req conversation.SendInteractiveRequest) (*conversation.SendOutcome, error) {
	body := req.ComposedBody()
	if body == "" {
		return nil, fmt.Errorf("%w: an interactive prompt needs a body", conversation.ErrCapabilityUnsupported)
	}
	if a.caps.TextTooLong(body) {
		return nil, fbdomain.ErrTextTooLong
	}

	style := channel.InteractiveStyleList
	if req.Style != channel.InteractiveStyleList && len(req.Options) <= a.caps.Interactive.MaxOptionsButtons {
		style = channel.InteractiveStyleButtons
	}
	kept, dropped := conversation.FitOptions(req.Options, a.caps.Interactive.MaxOptionsFor(style), a.caps.Interactive.MaxPayloadBytes)
	if len(kept) == 0 {
		return nil, fmt.Errorf("%w: no option could be rendered on messenger", conversation.ErrCapabilityUnsupported)
	}
	for _, d := range dropped {
		log.Printf("[facebook] option %q omitted: %s", d.ID, d.Reason)
	}

	target, err := a.target(ctx, ec, false)
	if err != nil {
		return nil, err
	}
	msg := fbdomain.OutboundMessage{Text: body}
	if style == channel.InteractiveStyleButtons {
		msg.Buttons = optionsOf(kept)
	} else {
		msg.QuickReplies = optionsOf(kept)
	}
	return a.dispatch(ctx, target, ec, msg)
}

func (a *ChannelAdapter) InteractiveLimits() channel.InteractiveLimits { return a.caps.Interactive }

func (a *ChannelAdapter) SendReaction(ctx context.Context, ec *conversation.EntryContext, targetProviderMessageID, reaction string) error {
	return a.withPage(ctx, ec, func(page *fbdomain.Page) error {
		return a.d.Messaging.React(ctx, page.FBPageID, page.PageToken, ec.ContactRef, targetProviderMessageID, reaction)
	})
}

func (a *ChannelAdapter) RemoveReaction(ctx context.Context, ec *conversation.EntryContext, targetProviderMessageID string) error {
	return a.withPage(ctx, ec, func(page *fbdomain.Page) error {
		return a.d.Messaging.Unreact(ctx, page.FBPageID, page.PageToken, ec.ContactRef, targetProviderMessageID)
	})
}

func (a *ChannelAdapter) SendTyping(ctx context.Context, ec *conversation.EntryContext, on bool) error {
	action := fbdomain.ActionTypingOff
	if on {
		action = fbdomain.ActionTypingOn
	}
	return a.senderAction(ctx, ec, action)
}

func (a *ChannelAdapter) MarkSeen(ctx context.Context, ec *conversation.EntryContext, _ string) error {
	return a.senderAction(ctx, ec, fbdomain.ActionMarkSeen)
}

func (a *ChannelAdapter) senderAction(ctx context.Context, ec *conversation.EntryContext, action fbdomain.SenderAction) error {
	return a.withPage(ctx, ec, func(page *fbdomain.Page) error {
		return a.d.Messaging.SendAction(ctx, page.FBPageID, page.PageToken, ec.ContactRef, action)
	})
}

func (a *ChannelAdapter) withPage(ctx context.Context, ec *conversation.EntryContext, call func(*fbdomain.Page) error) error {
	page, err := a.sendablePage(ctx, ec)
	if err != nil {
		return err
	}
	if err := call(page); err != nil {
		return a.classify(ctx, page, ec, err)
	}
	return nil
}

type sendTarget struct {
	page     *fbdomain.Page
	tier     fbdomain.SendTier
	human    bool
	metadata string
}

func (t sendTarget) withMetadata(metadata string) sendTarget {
	t.metadata = metadata
	return t
}

func (a *ChannelAdapter) target(ctx context.Context, ec *conversation.EntryContext, humanInitiated bool) (sendTarget, error) {
	page, err := a.sendablePage(ctx, ec)
	if err != nil {
		return sendTarget{}, err
	}
	window := conversation.EvaluateWindow(a.caps, ec.LastInboundAt, a.now(), humanInitiated)
	tier, ok := fbdomain.SendTierFor(window.Tier)
	if !window.Open || !ok {
		return sendTarget{}, conversation.ErrOutboundWindowClosed
	}
	return sendTarget{page: page, tier: tier, human: humanInitiated, metadata: fbdomain.OutboundMetadata(humanInitiated)}, nil
}

func (a *ChannelAdapter) sendablePage(ctx context.Context, ec *conversation.EntryContext) (*fbdomain.Page, error) {
	if ec == nil || ec.AccountID == "" {
		return nil, conversation.ErrNoAdapterForEntryType
	}
	page, err := a.d.Pages.FindByID(ctx, ec.AccountID)
	if err != nil {
		return nil, err
	}
	if !page.Can(fbdomain.CapMessaging) {
		return nil, fmt.Errorf("%w: page %s cannot message (status %s)", fbdomain.ErrCapabilityDenied, page.Name, page.Status)
	}
	if page.PageToken == "" {
		return nil, fbdomain.ErrAccessTokenRequired
	}
	return page, nil
}

func (a *ChannelAdapter) dispatch(ctx context.Context, t sendTarget, ec *conversation.EntryContext, msg fbdomain.OutboundMessage) (*conversation.SendOutcome, error) {
	msg.Recipient = fbdomain.Recipient{PSID: ec.ContactRef}
	msg.Tier = t.tier
	msg.Metadata = t.metadata

	result, err := a.sendOwningTheThread(ctx, t, ec, msg)
	if err != nil {
		log.Printf("[facebook] send FAILED page=%s recipient=%s: %v", t.page.FBPageID, ec.ContactRef, err)
		return nil, a.classify(ctx, t.page, ec, err)
	}
	if err := a.d.Conversations.RecordOutbound(ctx, ec.EntryID, a.now()); err != nil {
		log.Printf("[facebook] outbound not recorded conversation=%s: %v", ec.EntryID, err)
	}
	return &conversation.SendOutcome{ProviderMessageID: result.MessageID}, nil
}

func (a *ChannelAdapter) sendOwningTheThread(ctx context.Context, t sendTarget, ec *conversation.EntryContext, msg fbdomain.OutboundMessage) (*fbdomain.SendResult, error) {
	result, err := a.d.Messaging.Send(ctx, t.page.FBPageID, t.page.PageToken, msg)
	if fbdomain.Classify(err) != fbdomain.FailureThreadControl {
		return result, err
	}
	if !t.human {
		return nil, errors.Join(fbdomain.ErrThreadOwnedElsewhere, err)
	}
	if takeErr := a.d.Routing.TakeControl(ctx, t.page.FBPageID, t.page.PageToken, ec.ContactRef, msg.Metadata); takeErr != nil {
		return nil, errors.Join(fbdomain.ErrThreadOwnedElsewhere, takeErr)
	}
	if err := a.d.Conversations.SetThreadOwner(ctx, ec.EntryID, "", a.now()); err != nil {
		log.Printf("[facebook] thread owner not cleared conversation=%s: %v", ec.EntryID, err)
	}
	result, err = a.d.Messaging.Send(ctx, t.page.FBPageID, t.page.PageToken, msg)
	if fbdomain.Classify(err) == fbdomain.FailureThreadControl {
		return nil, errors.Join(fbdomain.ErrThreadOwnedElsewhere, err)
	}
	return result, err
}

var pageStatusReasons = map[fbdomain.Status]string{
	fbdomain.StatusTokenRevoked: "Facebook rejected the page token; reconnect required",
	fbdomain.StatusNeedsRole:    "the person who connected the page lost their role on it",
	fbdomain.StatusRestricted:   "Meta restricted this page from messaging",
}

func (a *ChannelAdapter) classify(ctx context.Context, page *fbdomain.Page, ec *conversation.EntryContext, err error) error {
	failure := fbdomain.Classify(err)
	if status, ok := fbdomain.PageStatusAfter(failure); ok && page.Status.CanTransitionTo(status) {
		if updateErr := a.d.Pages.UpdateStatus(ctx, page.ID, status, pageStatusReasons[status]); updateErr != nil {
			log.Printf("[facebook] page %s status could not be set to %s: %v", page.FBPageID, status, updateErr)
		}
	}

	switch failure {
	case fbdomain.FailureWindowClosed:
		return errors.Join(conversation.ErrOutboundWindowClosed, err)
	case fbdomain.FailureUnreachable:
		if markErr := a.d.Contacts.MarkUnreachable(ctx, ec.ContactID, string(failure)); markErr != nil {
			log.Printf("[facebook] contact %s not marked unreachable: %v", ec.ContactID, markErr)
		}
		return fmt.Errorf("this person can no longer receive messages from the page: %w", err)
	case fbdomain.FailureReauth, fbdomain.FailureRoleLost:
		return fmt.Errorf("facebook page %s needs to be reconnected: %w", page.Name, err)
	case fbdomain.FailurePageRestricted:
		return fmt.Errorf("facebook page %s is restricted from messaging: %w", page.Name, err)
	case fbdomain.FailureAccessLevel:
		return fmt.Errorf("the app is not yet approved by Meta to message this person: %w", err)
	}
	return err
}

func (a *ChannelAdapter) validateMedia(req conversation.SendMediaRequest) error {
	limit, ok := a.caps.MediaLimits[channel.MediaKind(req.Kind)]
	switch {
	case !ok:
		return fmt.Errorf("%w: messenger does not send %q", conversation.ErrCapabilityUnsupported, req.Kind)
	case req.URL == "" && len(req.Bytes) == 0:
		return fmt.Errorf("%w: media needs a URL or bytes", conversation.ErrCapabilityUnsupported)
	case req.MIMEType != "" && !limit.Allows(req.MIMEType):
		return fmt.Errorf("%w: messenger does not accept %s as %s", conversation.ErrCapabilityUnsupported, req.MIMEType, req.Kind)
	case int64(len(req.Bytes)) > limit.MaxBytes:
		return fmt.Errorf("%w: %s is over the %d byte messenger limit", conversation.ErrCapabilityUnsupported, req.Kind, limit.MaxBytes)
	}
	return nil
}

func optionsOf(options []conversation.InteractiveOption) []fbdomain.Option {
	out := make([]fbdomain.Option, 0, len(options))
	for _, o := range options {
		out = append(out, fbdomain.Option{Title: o.Title, Payload: o.ID})
	}
	return out
}
