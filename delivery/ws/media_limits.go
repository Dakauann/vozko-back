package ws

import (
	"vozko/domain/conversation"
	"vozko/domain/shared"
)

func (h *ConversationHub) SetMediaLimits(reader conversation.MediaLimitsReader) {
	h.mediaLimits = reader
}

func (h *ConversationHub) mediaLimitsOf(entryType string) *[]conversation.MediaLimit {
	if h.mediaLimits == nil {
		return nil
	}
	limits, known := h.mediaLimits.MediaLimits(shared.EntryType(entryType))
	if !known {
		return nil
	}
	if limits == nil {
		limits = []conversation.MediaLimit{}
	}
	return &limits
}
