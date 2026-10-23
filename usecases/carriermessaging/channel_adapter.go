package carriermessaging_usecase

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"vozko/domain/carriermessaging"
	"vozko/domain/channel"
	"vozko/domain/conversation"
	"vozko/domain/rcs"
	"vozko/domain/shared"
	"vozko/domain/sms"
)

type ChannelAdapter struct {
	svc     *Service
	channel carriermessaging.Channel
	caps    channel.Capabilities
}

func (s *Service) Adapter(c carriermessaging.Channel) *ChannelAdapter {
	caps := rcs.Capabilities()
	if c == carriermessaging.ChannelSMS {
		caps = sms.Capabilities()
	}
	return &ChannelAdapter{svc: s, channel: c, caps: caps}
}

func (a *ChannelAdapter) EntryType() shared.EntryType {
	return a.channel.EntryType()
}

func (a *ChannelAdapter) Capabilities() channel.Capabilities {
	return a.caps
}

func (a *ChannelAdapter) InteractiveLimits() channel.InteractiveLimits {
	return a.caps.Interactive
}

func (a *ChannelAdapter) ResolveEntry(ctx context.Context, entryID string) (*conversation.EntryContext, error) {
	conv, err := a.svc.conversations(a.channel).FindByID(ctx, entryID)
	if err != nil {
		return nil, err
	}
	contact, err := a.svc.Repos.Contacts.FindByID(ctx, conv.ContactID)
	if err != nil {
		return nil, err
	}
	return &conversation.EntryContext{
		EntryID: conv.ID, EntryType: a.EntryType(), WorkspaceID: conv.WorkspaceID, AccountID: conv.SenderID,
		ContactID: contact.ID, ContactRef: contact.Phone, ContactHandle: contact.Name(), LastInboundAt: conv.LastCustomerMessageAt,
	}, nil
}

func (a *ChannelAdapter) WindowState(ctx context.Context, ec *conversation.EntryContext) (conversation.WindowState, error) {
	sends, err := a.senderSends(ctx, ec.WorkspaceID, ec.AccountID)
	if err != nil {
		return conversation.WindowState{}, err
	}
	if !sends {
		return conversation.ClosedWindow(conversation.WindowReasonChannelUnavailable), nil
	}
	return conversation.OpenWindow(nil), nil
}

func (a *ChannelAdapter) WindowStates(ctx context.Context, entryIDs []string) (map[string]conversation.WindowState, error) {
	convs, err := a.svc.conversations(a.channel).FindByIDs(ctx, shared.UniqueNonEmpty(entryIDs))
	if err != nil {
		return nil, err
	}
	out := make(map[string]conversation.WindowState, len(convs))
	sending := map[string]bool{}
	for _, conv := range convs {
		sends, known := sending[conv.SenderID]
		if !known {
			if sends, err = a.senderSends(ctx, conv.WorkspaceID, conv.SenderID); err != nil {
				if !errors.Is(err, rcs.ErrSenderNotFound) && !errors.Is(err, sms.ErrRouteNotFound) {
					return nil, err
				}
				sends = false
			}
			sending[conv.SenderID] = sends
		}
		if sends {
			out[conv.ID] = conversation.OpenWindow(nil)
		} else {
			out[conv.ID] = conversation.ClosedWindow(conversation.WindowReasonChannelUnavailable)
		}
	}
	return out, nil
}

func (a *ChannelAdapter) senderSends(ctx context.Context, workspaceID, senderID string) (bool, error) {
	if a.channel == carriermessaging.ChannelSMS {
		route, err := a.svc.Repos.Routes.FindByID(ctx, workspaceID, senderID)
		if err != nil {
			return false, err
		}
		return route.CanSend(), nil
	}
	sender, err := a.svc.Repos.Senders.FindByID(ctx, workspaceID, senderID)
	if err != nil {
		return false, err
	}
	return sender.CanSend(), nil
}

func (a *ChannelAdapter) SendText(ctx context.Context, ec *conversation.EntryContext, req conversation.SendTextRequest) (*conversation.SendOutcome, error) {
	return a.send(ctx, ec, rcs.Content{Text: req.Body}, req.Body, req.HumanInitiated)
}

func (a *ChannelAdapter) SendMedia(ctx context.Context, ec *conversation.EntryContext, req conversation.SendMediaRequest) (*conversation.SendOutcome, error) {
	if a.channel != carriermessaging.ChannelRCS || !strings.HasPrefix(strings.TrimSpace(req.URL), "https://") {
		return nil, conversation.ErrCapabilityUnsupported
	}
	kind, ok := rcs.MediaKindFor(req.MIMEType)
	if !ok {
		return nil, conversation.ErrCapabilityUnsupported
	}
	content := rcs.Content{Text: req.Caption, Media: &rcs.Media{Kind: kind, URL: req.URL, MIMEType: req.MIMEType, Name: req.FileName}}
	return a.send(ctx, ec, content, "", req.HumanInitiated)
}

func (a *ChannelAdapter) SendInteractive(ctx context.Context, ec *conversation.EntryContext, req conversation.SendInteractiveRequest) (*conversation.SendOutcome, error) {
	if a.channel != carriermessaging.ChannelRCS {
		return nil, conversation.ErrCapabilityUnsupported
	}
	content := rcs.Content{Text: req.Body}
	for _, option := range req.Options {
		content.Suggestions = append(content.Suggestions, rcs.Suggestion{Kind: rcs.SuggestReply, Label: option.Title, Postback: option.ID})
	}
	return a.send(ctx, ec, content, "", false)
}

func (a *ChannelAdapter) send(ctx context.Context, ec *conversation.EntryContext, content rcs.Content, text string, human bool) (*conversation.SendOutcome, error) {
	kind := carriermessaging.RunWorkflow
	if human {
		kind = carriermessaging.RunOperator
	}
	msg, err := a.svc.Deliver(ctx, DeliverRequest{
		WorkspaceID: ec.WorkspaceID, Channel: a.channel, SenderID: ec.AccountID, Phone: ec.ContactRef,
		ContactName: ec.ContactHandle, RunKind: kind, RunID: ec.EntryID, Content: content, Text: text,
		RecordedByCaller: true,
	})
	if err != nil {
		return nil, err
	}
	if msg.Status.Terminal() {
		return nil, fmt.Errorf("%w: %s", carriermessaging.ErrProviderRefused, msg.ErrorCode)
	}
	return &conversation.SendOutcome{ProviderMessageID: msg.Reference, DeliveryStatus: transcriptStatus(msg.Status)}, nil
}
