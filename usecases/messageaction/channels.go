package messageaction

import (
	"vozko/domain/conversation"
	"vozko/domain/shared"
)

type Channels struct {
	Adapters conversation.AdapterRegistry
	Official conversation.MessageActionChannel
}

func (c Channels) For(entryType shared.EntryType) (conversation.MessageActionChannel, bool) {
	if entryType == shared.EntryTypeWhatsApp {
		return c.Official, c.Official != nil
	}
	if c.Adapters == nil {
		return nil, false
	}
	adapter, err := c.Adapters.For(entryType)
	if err != nil {
		return nil, false
	}
	return adapter, true
}

func (c Channels) MessageCapabilities(entryType shared.EntryType) conversation.MessageCapabilities {
	channel, ok := c.For(entryType)
	if !ok {
		return conversation.MessageCapabilities{}
	}
	return conversation.MessageCapabilitiesOf(channel)
}
