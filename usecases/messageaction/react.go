package messageaction

import (
	"context"
	"strings"

	"vozko/domain/conversation"
	"vozko/domain/shared"
)

type react struct {
	deps Deps
}

func (uc react) Execute(ctx context.Context, by shared.Person, target conversation.MessageTarget, emoji string) (*conversation.Message, error) {
	prepared, err := uc.deps.prepare(ctx, by, target, conversation.MessageActionReact)
	if err != nil {
		return nil, err
	}
	reacting, ok := prepared.channel.(conversation.ReactingAdapter)
	if !ok {
		return nil, conversation.ErrMessageActionUnsupported
	}

	emoji = strings.TrimSpace(emoji)
	providerID := prepared.message.ProviderMessageID()
	if emoji == "" {
		err = reacting.RemoveReaction(ctx, prepared.entry, providerID)
	} else {
		err = reacting.SendReaction(ctx, prepared.entry, providerID, emoji)
	}
	if err != nil {
		return nil, err
	}

	if err := prepared.message.React(conversation.Reaction{
		Emoji: emoji, Direction: conversation.ReactionOutbound, ActorID: by.UserID, At: uc.deps.Now(),
	}); err != nil {
		return nil, err
	}
	return uc.deps.save(prepared.message)
}
