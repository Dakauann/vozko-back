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

type QuotedMessages interface {
	GetByEntryAndExternalMessageID(entryType shared.EntryType, entryID, externalID string) (*conversation.Message, error)
}

type channelAdapter struct {
	widgets       wcdomain.WidgetRepository
	visitors      wcdomain.VisitorRepository
	conversations wcdomain.ConversationRepository
	events        wcdomain.EventPublisher
	quotes        QuotedMessages
	now           func() time.Time
	caps          channel.Capabilities
}

func NewChannelAdapter(
	widgets wcdomain.WidgetRepository,
	visitors wcdomain.VisitorRepository,
	conversations wcdomain.ConversationRepository,
	events wcdomain.EventPublisher,
	quotes QuotedMessages,
) conversation.ChannelAdapter {
	return &channelAdapter{
		quotes:        quotes,
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
	widget, err := a.widgets.FindByIDUnscoped(ctx, ec.AccountID)
	if err != nil {
		return conversation.ClosedWindow(conversation.WindowReasonChannelUnavailable), nil
	}
	visitor, err := a.visitors.FindByID(ctx, ec.ContactID)
	if err != nil {
		return conversation.ClosedWindow(conversation.WindowReasonChannelUnavailable), err
	}
	return windowOf(widget, visitor), nil
}

func (a *channelAdapter) WindowStates(ctx context.Context, entryIDs []string) (map[string]conversation.WindowState, error) {
	convs, err := a.conversations.FindByIDs(ctx, shared.UniqueNonEmpty(entryIDs))
	if err != nil {
		return nil, err
	}
	widgetIDs := make([]string, 0, len(convs))
	visitorIDs := make([]string, 0, len(convs))
	for _, conv := range convs {
		widgetIDs = append(widgetIDs, conv.WidgetID)
		visitorIDs = append(visitorIDs, conv.VisitorID)
	}
	widgets, err := a.widgets.FindByIDsUnscoped(ctx, shared.UniqueNonEmpty(widgetIDs))
	if err != nil {
		return nil, err
	}
	widgetByID := make(map[string]*wcdomain.Widget, len(widgets))
	for _, widget := range widgets {
		widgetByID[widget.ID] = widget
	}
	visitors, err := a.visitors.FindByIDs(ctx, shared.UniqueNonEmpty(visitorIDs))
	if err != nil {
		return nil, err
	}
	visitorByID := make(map[string]*wcdomain.Visitor, len(visitors))
	for _, visitor := range visitors {
		visitorByID[visitor.ID] = visitor
	}
	out := make(map[string]conversation.WindowState, len(convs))
	for _, conv := range convs {
		out[conv.ID] = windowOf(widgetByID[conv.WidgetID], visitorByID[conv.VisitorID])
	}
	return out, nil
}

func windowOf(widget *wcdomain.Widget, visitor *wcdomain.Visitor) conversation.WindowState {
	if widget == nil || visitor == nil {
		return conversation.ClosedWindow(conversation.WindowReasonChannelUnavailable)
	}
	if visitor.Blocked {
		return conversation.ClosedWindow(conversation.WindowReasonContactBlocked)
	}
	return conversation.OpenWindow(nil)
}

var _ conversation.WindowStatesAdapter = (*channelAdapter)(nil)

func (a *channelAdapter) Capabilities() channel.Capabilities { return a.caps }

var _ conversation.CapabilitiesAdapter = (*channelAdapter)(nil)

func (a *channelAdapter) SendText(ctx context.Context, ec *conversation.EntryContext, req conversation.SendTextRequest) (*conversation.SendOutcome, error) {
	body := strings.TrimSpace(req.Body)
	if body == "" {
		return nil, wcdomain.ErrMessageEmpty
	}
	if a.caps.TextTooLong(body) {
		return nil, wcdomain.ErrMessageTooLong
	}
	return a.deliver(ctx, ec, wcdomain.VisitorMessage{Author: authorFor(req.HumanInitiated), Text: body, Quote: a.quoteOf(ec, req.ReplyToProviderMessageID)})
}

func (a *channelAdapter) SendMedia(ctx context.Context, ec *conversation.EntryContext, req conversation.SendMediaRequest) (*conversation.SendOutcome, error) {
	if strings.TrimSpace(req.URL) == "" {
		return nil, fmt.Errorf("%w: webchat media needs a stored file", conversation.ErrCapabilityUnsupported)
	}
	return a.deliver(ctx, ec, wcdomain.VisitorMessage{
		Author: authorFor(req.HumanInitiated),
		Text:   strings.TrimSpace(req.Caption),
		Quote:  a.quoteOf(ec, req.ReplyToProviderMessageID),
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

func (a *channelAdapter) quoteOf(ec *conversation.EntryContext, providerID string) *wcdomain.VisitorQuote {
	if ec == nil || strings.TrimSpace(providerID) == "" {
		return nil
	}
	quoted, err := a.quotes.GetByEntryAndExternalMessageID(shared.EntryTypeWebchat, ec.EntryID, providerID)
	if err != nil {
		return nil
	}
	return wcdomain.QuoteOf(quoted)
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
