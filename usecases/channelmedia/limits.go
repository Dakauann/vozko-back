package channelmedia

import (
	"vozko/domain/channel"
	"vozko/domain/conversation"
	"vozko/domain/shared"
)

type Limits struct {
	Adapters conversation.AdapterRegistry
	Official map[channel.MediaKind]channel.MediaLimit
}

var _ conversation.MediaLimitsReader = Limits{}

func (l Limits) MediaLimits(entryType shared.EntryType) ([]conversation.MediaLimit, bool) {
	if entryType == shared.EntryTypeWhatsApp {
		if l.Official == nil {
			return nil, false
		}
		return conversation.MediaLimitsOf(l.Official), true
	}
	caps, err := conversation.CapabilitiesOf(l.Adapters, entryType)
	if err != nil {
		return nil, false
	}
	return conversation.MediaLimitsOf(caps.MediaLimits), true
}
