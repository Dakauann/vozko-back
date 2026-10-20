package tiktok

import (
	"vozko/domain/channel"
	"vozko/domain/shared"
)

func Descriptor() *channel.Descriptor {
	return &channel.Descriptor{
		Kind:         channel.KindTikTok,
		EntryType:    shared.EntryTypeTikTok,
		Capabilities: ChannelCapabilities(),
	}
}
