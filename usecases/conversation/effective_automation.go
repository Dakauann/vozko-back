package conversation_usecase

import (
	"context"

	"vozko/domain/conversation"
	"vozko/domain/shared"
)

func EffectiveAutomation(
	ctx context.Context,
	delegations conversation.DelegationRepository,
	entryID string,
	entryType shared.EntryType,
	channel conversation.ChannelAutomation,
) (conversation.ChannelAutomation, error) {
	if delegations == nil {
		return channel, nil
	}
	delegation, err := delegations.Find(ctx, entryID, entryType)
	if err != nil {
		return conversation.ChannelAutomation{}, err
	}
	if delegation == nil {
		return channel, nil
	}
	return channel.DelegatedTo(delegation.Automation), nil
}
