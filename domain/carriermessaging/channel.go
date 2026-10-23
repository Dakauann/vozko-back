package carriermessaging

import (
	"slices"

	"vozko/domain/shared"
)

type Channel string

const (
	ChannelRCS Channel = "rcs"
	ChannelSMS Channel = "sms"
)

func AllChannels() []Channel {
	return []Channel{ChannelRCS, ChannelSMS}
}

var launchedChannels = []Channel{}

func (c Channel) Launched() bool {
	return slices.Contains(launchedChannels, c)
}

func LaunchedChannels() []Channel {
	var out []Channel
	for _, c := range AllChannels() {
		if c.Launched() {
			out = append(out, c)
		}
	}
	return out
}

func (c Channel) Valid() bool {
	return c == ChannelRCS || c == ChannelSMS
}

func (c Channel) EntryType() shared.EntryType {
	switch c {
	case ChannelRCS:
		return shared.EntryTypeRCS
	case ChannelSMS:
		return shared.EntryTypeSMS
	}
	return ""
}

func (c Channel) InboundCharge() ChargeKind {
	if c == ChannelSMS {
		return ChargeSMSInbound
	}
	return ChargeRCSInbound
}
