package acme

import "vozko/domain/shared"

type ChannelAdapter struct{}

func (a *ChannelAdapter) EntryType() shared.EntryType { return "acme" }
