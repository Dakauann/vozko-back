package messageaction

import (
	"context"
	"strings"

	"vozko/domain/conversation"
	"vozko/domain/shared"
)

type edit struct {
	deps Deps
}

func (uc edit) Execute(ctx context.Context, by shared.Person, target conversation.MessageTarget, text string) (*conversation.Message, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, conversation.ErrMessageEditTextInvalid
	}
	prepared, err := uc.deps.prepare(ctx, by, target, conversation.MessageActionEdit)
	if err != nil {
		return nil, err
	}
	editing, ok := prepared.channel.(conversation.EditingAdapter)
	if !ok {
		return nil, conversation.ErrMessageActionUnsupported
	}
	if err := editing.EditText(ctx, prepared.entry, prepared.message.ProviderMessageID(), text); err != nil {
		return nil, err
	}

	prepared.message.ApplyEdit(text, uc.deps.Now())
	return uc.deps.save(prepared.message)
}
