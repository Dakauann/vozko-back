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

	body := req.ComposedBody()
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
		log.Printf("[instagram] option %q omitted from quick replies: %s", d.ID, d.Reason)
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

func quickReplyOptionsFor(options []conversation.InteractiveOption) ([]igdomain.QuickReplyOption, []conversation.DroppedOption) {
	kept, dropped := conversation.FitOptions(options, igdomain.MaxQuickReplies, igdomain.MaxQuickReplyPayloadBytes)
	out := make([]igdomain.QuickReplyOption, 0, len(kept))
	for _, opt := range kept {
		out = append(out, igdomain.QuickReplyOption{Title: opt.Title, Payload: opt.ID})
	}
	return out, dropped
}

var _ conversation.InteractiveAdapter = (*channelAdapter)(nil)
