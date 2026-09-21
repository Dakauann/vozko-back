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

	body := composeInteractiveBody(req)
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
		log.Printf("[telegram] option %q omitted from inline keyboard: %s", d.id, d.reason)
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

type droppedOption struct {
	id     string
	reason string
}

func inlineKeyboardFor(options []conversation.InteractiveOption) ([][]inlineButton, []droppedOption) {
	rows := make([][]inlineButton, 0, len(options))
	var dropped []droppedOption

	for _, opt := range options {
		id := strings.TrimSpace(opt.ID)
		title := strings.TrimSpace(opt.Title)
		if title == "" {
			title = id
		}

		switch {
		case id == "":
			dropped = append(dropped, droppedOption{opt.Title, "no id to send back on press"})
			continue
		case len(id) > tgdomain.MaxCallbackDataBytes:
			dropped = append(dropped, droppedOption{id, fmt.Sprintf(
				"callback_data is %d bytes, over Telegram's %d-byte limit",
				len(id), tgdomain.MaxCallbackDataBytes)})
			continue
		case len(rows) >= tgdomain.MaxInlineKeyboardButtons:
			dropped = append(dropped, droppedOption{id, "beyond the inline keyboard cap"})
			continue
		}

		rows = append(rows, []inlineButton{{Text: title, CallbackData: id}})
	}

	return rows, dropped
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
