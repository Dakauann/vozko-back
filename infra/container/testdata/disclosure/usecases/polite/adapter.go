package polite

import (
	"context"

	"vozko/domain/conversation"
	"vozko/domain/shared"
)

type channelAdapter struct{}

func (a *channelAdapter) EntryType() shared.EntryType { return "polite" }

func (a *channelAdapter) AutomationDisclosure(context.Context, *conversation.EntryContext) (string, error) {
	return "", nil
}
