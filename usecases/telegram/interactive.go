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

// inlineButton is one entry of Telegram's inline_keyboard.
//
// Only text + callback_data are set. url buttons open a link instead of
// answering, which is a different product feature; a workflow that branches on
// the answer needs the answer to come back.
type inlineButton struct {
	Text         string `json:"text"`
	CallbackData string `json:"callback_data"`
}

// SendInteractive renders the options as an inline keyboard.
//
// Telegram is the most permissive channel we carry on option COUNT and the
// strictest on option PAYLOAD: the Bot API documents no bound on
// inline_keyboard length, but callback_data is "1-64 bytes". So the failure
// mode here is not "too many options" — it is an option id that is a few
// accented characters too long, which would fail the whole send.
//
// Options whose id does not fit are dropped rather than truncated. A truncated
// callback payload comes back as an id that matches no branch, which sends the
// run down the no_match path and looks like the customer answered something
// unexpected; dropping the option means they are never offered a button that
// could not have worked.
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
		ProviderMessageID: tgdomain.ProviderMessageID(result.ChatID, result.MessageID),
	}, nil
}

// InteractiveLimits reports Telegram's own bounds, read from the descriptor so
// there is exactly one place they are written down.
func (a *channelAdapter) InteractiveLimits() channel.InteractiveLimits {
	return a.caps.Interactive
}

type droppedOption struct {
	id     string
	reason string
}

// inlineKeyboardFor lays the options out one per row.
//
// Single column is deliberate: Telegram does not bound the label length, so an
// author's long option would be truncated unpredictably in a two-column layout
// while a single column always renders in full.
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
			// Bytes, not characters — the documented limit is on the encoded
			// payload, so an id of accented text overflows earlier than it looks.
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

// composeInteractiveBody folds header and footer into the message text.
//
// Telegram has no header or footer slot. Dropping them would silently discard
// words the author wrote and expected the customer to read, so they are joined
// into the body instead — the header emphasised, the footer plain.
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

// compile-time proof that the adapter satisfies the optional capability.
var _ conversation.InteractiveAdapter = (*channelAdapter)(nil)
