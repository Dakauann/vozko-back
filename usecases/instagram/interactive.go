package instagram

import (
	"context"
	"fmt"
	"log"
	"strings"

	"vozko/domain/channel"
	"vozko/domain/conversation"
	igdomain "vozko/domain/instagram"
)

func (a *channelAdapter) SendInteractive(
	ctx context.Context,
	ec *conversation.EntryContext,
	req conversation.SendInteractiveRequest,
) (*conversation.SendOutcome, error) {
	account, err := a.sendableAccount(ctx, ec)
	if err != nil {
		return nil, err
	}
	if err := a.assertWindowOpen(ctx, ec); err != nil {
		return nil, err
	}

	body := composeInteractiveBody(req)
	if strings.TrimSpace(body) == "" {
		return nil, fmt.Errorf("%w: an interactive prompt needs a body", conversation.ErrCapabilityUnsupported)
	}
	if len(body) > a.caps.MaxTextBytes {
		return nil, igdomain.ErrTextTooLong
	}

	options, dropped := quickReplyOptionsFor(req.Options)
	if len(options) == 0 {
		return nil, fmt.Errorf("%w: no option could be rendered as a quick reply", conversation.ErrCapabilityUnsupported)
	}
	for _, d := range dropped {
		log.Printf("[instagram] option %q omitted from quick replies: %s", d.id, d.reason)
	}

	result, err := a.messaging.SendText(ctx, account.IGUserID, account.AccessToken, igdomain.SendTextInput{
		RecipientIGSID: ec.ContactRef,
		Text:           body,
		QuickReplies:   options,
	})
	if err != nil {
		log.Printf("[instagram] send quick replies FAILED account=@%s recipient=%s: %v",
			account.Username, ec.ContactRef, err)
		return nil, a.classify(ctx, account, err)
	}
	log.Printf("[instagram] sent quick replies account=@%s recipient=%s mid=%s options=%d",
		account.Username, ec.ContactRef, result.MessageID, len(options))

	a.recordOutbound(ctx, ec)
	return &conversation.SendOutcome{ProviderMessageID: result.MessageID}, nil
}

func (a *channelAdapter) InteractiveLimits() channel.InteractiveLimits {
	return a.caps.Interactive
}

type droppedOption struct {
	id     string
	reason string
}

func quickReplyOptionsFor(options []conversation.InteractiveOption) ([]igdomain.QuickReplyOption, []droppedOption) {
	out := make([]igdomain.QuickReplyOption, 0, len(options))
	var dropped []droppedOption

	for _, opt := range options {
		id := strings.TrimSpace(opt.ID)
		title := strings.TrimSpace(opt.Title)
		if title == "" {
			title = id
		}

		switch {
		case id == "":
			dropped = append(dropped, droppedOption{opt.Title, "no payload to send back on tap"})
			continue
		case len(id) > igdomain.MaxQuickReplyPayloadBytes:
			dropped = append(dropped, droppedOption{id, fmt.Sprintf(
				"payload is %d bytes, over the %d-byte limit",
				len(id), igdomain.MaxQuickReplyPayloadBytes)})
			continue
		case len(out) >= igdomain.MaxQuickReplies:
			dropped = append(dropped, droppedOption{id, fmt.Sprintf(
				"beyond Instagram's %d quick replies", igdomain.MaxQuickReplies)})
			continue
		}

		out = append(out, igdomain.QuickReplyOption{Title: title, Payload: id})
	}

	return out, dropped
}

func composeInteractiveBody(req conversation.SendInteractiveRequest) string {
	parts := make([]string, 0, 3)
	if h := strings.TrimSpace(req.Header); h != "" {
		parts = append(parts, h)
	}
	if b := strings.TrimSpace(req.Body); b != "" {
		parts = append(parts, b)
	}
	if f := strings.TrimSpace(req.Footer); f != "" {
		parts = append(parts, f)
	}
	return strings.Join(parts, "\n\n")
}

var _ conversation.InteractiveAdapter = (*channelAdapter)(nil)
