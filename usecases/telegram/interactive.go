package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"log"
	"strings"

	"vozko/domain/channel"
	"vozko/domain/conversation"
	tgdomain "vozko/domain/telegram"
)

type inlineButton struct {
	Text         string `json:"text"`
	CallbackData string `json:"callback_data"`
}

func (a *channelAdapter) SendInteractive(
	ctx context.Context,
	ec *conversation.EntryContext,
	req conversation.SendInteractiveRequest,
) (*conversation.SendOutcome, error) {
	account, conv, err := a.sendable(ctx, ec)
	if err != nil {
		return nil, err
	}

	body := req.ComposedBody()
	if strings.TrimSpace(body) == "" {
		return nil, fmt.Errorf("%w: an interactive prompt needs a body", conversation.ErrCapabilityUnsupported)
	}
	if a.caps.TextTooLong(body) {
		return nil, tgdomain.ErrTextTooLong
	}

	rows, dropped := inlineKeyboardFor(req.Options)
	if len(rows) == 0 {
		return nil, fmt.Errorf("%w: no option could be rendered as an inline button", conversation.ErrCapabilityUnsupported)
	}
	for _, d := range dropped {
		log.Printf("[telegram] option %q omitted from inline keyboard: %s", d.ID, d.Reason)
	}

	markup, err := json.Marshal(map[string]any{"inline_keyboard": rows})
	if err != nil {
		return nil, err
	}

	in := tgdomain.SendTextInput{
		ChatID:               conv.TGChatID,
		Text:                 html.EscapeString(body),
		ParseMode:            "HTML",
		BusinessConnectionID: businessConnectionOf(account, conv),
		ReplyMarkup:          string(markup),
	}

	result, err := a.api.SendText(ctx, account.BotToken, in)
	if err != nil {
		return nil, a.classify(ctx, account, conv, ec, err)
	}

	log.Printf("[telegram] sent inline keyboard account=@%s chat=%d message_id=%d options=%d",
		account.BotUsername, result.ChatID, result.MessageID, len(rows))
	a.recordOutbound(ctx, ec)

	return &conversation.SendOutcome{
		ProviderMessageID: tgdomain.ProviderMessageID(account.BotUserID, result.ChatID, result.MessageID),
	}, nil
}

func (a *channelAdapter) InteractiveLimits() channel.InteractiveLimits {
	return a.caps.Interactive
}

func inlineKeyboardFor(options []conversation.InteractiveOption) ([][]inlineButton, []conversation.DroppedOption) {
	kept, dropped := conversation.FitOptions(options, tgdomain.MaxInlineKeyboardButtons, tgdomain.MaxCallbackDataBytes)
	rows := make([][]inlineButton, 0, len(kept))
	for _, opt := range kept {
		rows = append(rows, []inlineButton{{Text: opt.Title, CallbackData: opt.ID}})
	}
	return rows, dropped
}

var _ conversation.InteractiveAdapter = (*channelAdapter)(nil)
