package webchat

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"vozko/domain/channel"
	"vozko/domain/conversation"
	"vozko/domain/shared"
	wcdomain "vozko/domain/webchat"
)

type channelAdapter struct {
	widgets       wcdomain.WidgetRepository
	visitors      wcdomain.VisitorRepository
	conversations wcdomain.ConversationRepository
	events        wcdomain.EventPublisher
	now           func() time.Time
	caps          channel.Capabilities
}

func NewChannelAdapter(
	widgets wcdomain.WidgetRepository,
	visitors wcdomain.VisitorRepository,
	conversations wcdomain.ConversationRepository,
	events wcdomain.EventPublisher,
) conversation.ChannelAdapter {
	return &channelAdapter{
		widgets:       widgets,
		visitors:      visitors,
		conversations: conversations,
		events:        events,
		now:           func() time.Time { return time.Now().UTC() },
		caps:          wcdomain.Descriptor().Capabilities,
	}
}

func (a *channelAdapter) EntryType() shared.EntryType { return shared.EntryTypeWebchat }

func (a *channelAdapter) ResolveEntry(ctx context.Context, entryID string) (*conversation.EntryContext, error) {
	conv, err := a.conversations.FindByID(ctx, entryID)
	if err != nil {
		return nil, err
	}
	visitor, err := a.visitors.FindByID(ctx, conv.VisitorID)
	if err != nil {
		return nil, err
	}
	return &conversation.EntryContext{
		EntryID:       conv.ID,
		EntryType:     shared.EntryTypeWebchat,
		WorkspaceID:   conv.WorkspaceID,
		AccountID:     conv.WidgetID,
		ContactID:     visitor.ID,
		ContactRef:    visitor.ID,
		ContactHandle: visitor.Handle(),
		LastInboundAt: conv.LastCustomerMessageAt,
	}, nil
}

func (a *channelAdapter) WindowState(ctx context.Context, ec *conversation.EntryContext) (conversation.WindowState, error) {
	if ec == nil {
		return conversation.ClosedWindow(conversation.WindowReasonChannelUnavailable), conversation.ErrNoAdapterForEntryType
	}
	if _, err := a.widgets.FindByIDUnscoped(ctx, ec.AccountID); err != nil {
		return conversation.ClosedWindow(conversation.WindowReasonChannelUnavailable), nil
	}
	visitor, err := a.visitors.FindByID(ctx, ec.ContactID)
	if err != nil {
		return conversation.ClosedWindow(conversation.WindowReasonChannelUnavailable), err
	}
	if visitor.Blocked {
		return conversation.ClosedWindow(conversation.WindowReasonContactBlocked), nil
	}
	return conversation.OpenWindow(nil), nil
}

func (a *channelAdapter) SendText(ctx context.Context, ec *conversation.EntryContext, req conversation.SendTextRequest) (*conversation.SendOutcome, error) {
	body := strings.TrimSpace(req.Body)
	if body == "" {
		return nil, wcdomain.ErrMessageEmpty
	}
	if a.caps.TextTooLong(body) {
		return nil, wcdomain.ErrMessageTooLong
	}
	return a.deliver(ctx, ec, wcdomain.VisitorMessage{Author: authorFor(req.HumanInitiated), Text: body})
}

func (a *channelAdapter) SendMedia(ctx context.Context, ec *conversation.EntryContext, req conversation.SendMediaRequest) (*conversation.SendOutcome, error) {
	if strings.TrimSpace(req.URL) == "" {
		return nil, fmt.Errorf("%w: webchat media needs a stored file", conversation.ErrCapabilityUnsupported)
	}
	return a.deliver(ctx, ec, wcdomain.VisitorMessage{
		Author: authorFor(req.HumanInitiated),
		Text:   strings.TrimSpace(req.Caption),
		Media: &wcdomain.VisitorMedia{
			Kind:     req.Kind,
			URL:      req.URL,
			MimeType: req.MIMEType,
			Filename: req.FileName,
		},
	})
}

func (a *channelAdapter) SendInteractive(ctx context.Context, ec *conversation.EntryContext, req conversation.SendInteractiveRequest) (*conversation.SendOutcome, error) {
	body := req.ComposedBody()
	if strings.TrimSpace(body) == "" {
		return nil, fmt.Errorf("%w: an interactive prompt needs a body", conversation.ErrCapabilityUnsupported)
	}
	kept, dropped := conversation.FitOptions(req.Options, wcdomain.MaxPendingOptions, wcdomain.MaxOptionIDBytes)
	for _, d := range dropped {
		log.Printf("[webchat] option %q omitted: %s", d.ID, d.Reason)
	}
	options := make([]wcdomain.Option, 0, len(kept))
	for _, o := range kept {
		options = append(options, wcdomain.Option{ID: o.ID, Title: o.Title})
	}
	options = wcdomain.BoundOptions(options)
	if len(options) == 0 {
		return nil, fmt.Errorf("%w: no option could be offered", conversation.ErrCapabilityUnsupported)
	}
	if err := a.conversations.SetPendingOptions(ctx, ec.EntryID, options); err != nil {
		return nil, err
	}
	return a.deliver(ctx, ec, wcdomain.VisitorMessage{Author: wcdomain.AuthorAssistant, Text: body, Options: options})
}

func (a *channelAdapter) InteractiveLimits() channel.InteractiveLimits {
	return a.caps.Interactive
}

func (a *channelAdapter) SendTyping(ctx context.Context, ec *conversation.EntryContext, on bool) error {
	if ec == nil {
		return conversation.ErrNoAdapterForEntryType
	}
	return a.events.Publish(ctx, wcdomain.VisitorEvent{VisitorID: ec.ContactID, Kind: wcdomain.EventTyping, Typing: on})
}

func (a *channelAdapter) MarkSeen(context.Context, *conversation.EntryContext, string) error {
	return nil
}

func (a *channelAdapter) deliver(ctx context.Context, ec *conversation.EntryContext, message wcdomain.VisitorMessage) (*conversation.SendOutcome, error) {
	if ec == nil {
		return nil, conversation.ErrNoAdapterForEntryType
	}
	message.ID = wcdomain.NewOutboundProviderID()
	message.CreatedAt = a.now()
	if err := a.events.Publish(ctx, wcdomain.VisitorEvent{VisitorID: ec.ContactID, Kind: wcdomain.EventMessage, Message: &message}); err != nil {
		log.Printf("[webchat] live delivery to visitor=%s failed, the message reaches them on the next sync: %v", ec.ContactID, err)
	}
	if err := a.conversations.RecordOutbound(ctx, ec.EntryID, message.CreatedAt); err != nil {
		log.Printf("[webchat] record outbound conversation=%s: %v", ec.EntryID, err)
	}
	return &conversation.SendOutcome{ProviderMessageID: message.ID}, nil
}

func authorFor(humanInitiated bool) wcdomain.Author {
	if humanInitiated {
		return wcdomain.AuthorTeam
	}
	return wcdomain.AuthorAssistant
}

var (
	_ conversation.InteractiveAdapter = (*channelAdapter)(nil)
	_ conversation.PresenceAdapter    = (*channelAdapter)(nil)
)
