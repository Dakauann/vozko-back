package messageaction

import (
	"context"

	"vozko/domain/conversation"
	"vozko/domain/shared"
)

type retract struct {
	deps Deps
}

func (uc retract) Execute(ctx context.Context, by shared.Person, target conversation.MessageTarget) (*conversation.Message, error) {
	prepared, err := uc.deps.prepare(ctx, by, target, conversation.MessageActionDelete)
	if err != nil {
		return nil, err
	}
	retracting, ok := prepared.channel.(conversation.RetractingAdapter)
	if !ok {
		return nil, conversation.ErrMessageActionUnsupported
	}
	if err := retracting.Retract(ctx, prepared.entry, prepared.message.ProviderMessageID()); err != nil {
		return nil, err
	}

	prepared.message.Retract(uc.deps.Now())
	return uc.deps.save(prepared.message)
}
